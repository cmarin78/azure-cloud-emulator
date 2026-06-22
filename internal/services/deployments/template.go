package deployments

import (
	"encoding/json"
	"fmt"
	"strings"
)

// template replica el subconjunto de un ARM template (JSON nativo, o el
// JSON compilado a partir de un archivo Bicep — `az bicep build` produce
// exactamente este mismo shape, así que no hace falta distinguir entre
// ambos) que esta fase necesita: parámetros, variables, el arreglo de
// recursos y un bloque de outputs que se acepta pero no se evalúa (ver
// ROADMAP.md, Phase 19, para el alcance deliberadamente incompleto).
type template struct {
	Parameters map[string]templateParameter `json:"parameters,omitempty"`
	Variables  map[string]json.RawMessage   `json:"variables,omitempty"`
	Resources  []templateResource           `json:"resources"`
	Outputs    map[string]json.RawMessage   `json:"outputs,omitempty"`
}

type templateParameter struct {
	Type         string          `json:"type,omitempty"`
	DefaultValue json.RawMessage `json:"defaultValue,omitempty"`
}

// templateResource replica una entrada de template.resources[]. Properties/
// SKU/Identity se mantienen como json.RawMessage porque su shape depende
// del tipo de recurso — se resuelven genéricamente (ver resolveJSON) y se
// reenvían tal cual al handler PUT existente del servicio correspondiente.
type templateResource struct {
	Type       string            `json:"type"`
	APIVersion string            `json:"apiVersion"`
	Name       string            `json:"name"`
	Location   string            `json:"location,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	SKU        json.RawMessage   `json:"sku,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Identity   json.RawMessage   `json:"identity,omitempty"`
	Properties json.RawMessage   `json:"properties,omitempty"`
	DependsOn  []string          `json:"dependsOn,omitempty"`
}

// evalContext agrupa lo necesario para resolver expresiones ARM
// (`[...]`) dentro de un template: el alcance (subscription/resource
// group) en el que corre el deployment, y los valores ya resueltos de
// parameters/variables.
type evalContext struct {
	subID, rg  string
	parameters map[string]any
	variables  map[string]any
}

// resolveParameters combina los valores enviados en
// properties.parameters del PUT del deployment con
// template.parameters[*].defaultValue, y falla si falta un parámetro
// requerido sin default — igual que `az deployment group create` real.
func resolveParameters(tmpl *template, supplied map[string]deploymentParameterValue) (map[string]any, error) {
	resolved := make(map[string]any, len(tmpl.Parameters))
	for name, decl := range tmpl.Parameters {
		if sv, ok := supplied[name]; ok && len(sv.Value) > 0 {
			var v any
			if err := json.Unmarshal(sv.Value, &v); err != nil {
				return nil, fmt.Errorf("parámetro %q: valor inválido: %w", name, err)
			}
			resolved[name] = v
			continue
		}
		if len(decl.DefaultValue) > 0 {
			var v any
			if err := json.Unmarshal(decl.DefaultValue, &v); err != nil {
				return nil, fmt.Errorf("parámetro %q: defaultValue inválido: %w", name, err)
			}
			resolved[name] = v
			continue
		}
		return nil, fmt.Errorf("falta el parámetro requerido %q (sin defaultValue en el template)", name)
	}
	return resolved, nil
}

// resolveVariables evalúa template.variables[*] en el orden declarado.
// Solo soporta variables cuyo valor sea un literal o una única expresión
// `[...]` que referencie parameters()/variables() ya resueltas — el mismo
// subconjunto deliberadamente incompleto que el resto de esta fase (ver
// ROADMAP.md).
func resolveVariables(tmpl *template, ctx *evalContext) error {
	ctx.variables = make(map[string]any, len(tmpl.Variables))
	for name, raw := range tmpl.Variables {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("variable %q: valor inválido: %w", name, err)
		}
		resolved, err := resolveValue(v, ctx)
		if err != nil {
			return fmt.Errorf("variable %q: %w", name, err)
		}
		ctx.variables[name] = resolved
	}
	return nil
}

// resolveValue camina recursivamente un valor JSON ya deserializado
// (string/float64/bool/map[string]any/[]any/nil) reemplazando cualquier
// string que sea una expresión ARM completa (`"[...]"`) por su valor
// evaluado — que puede ser de cualquier tipo, no solo string (p. ej.
// `"[parameters('count')]"` con count numérico se reemplaza por el
// número, no por su representación en texto).
func resolveValue(v any, ctx *evalContext) (any, error) {
	switch t := v.(type) {
	case string:
		return evalExpression(t, ctx)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			rv, err := resolveValue(vv, ctx)
			if err != nil {
				return nil, err
			}
			out[k] = rv
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			rv, err := resolveValue(vv, ctx)
			if err != nil {
				return nil, err
			}
			out[i] = rv
		}
		return out, nil
	default:
		return v, nil
	}
}

// resolveJSON aplica resolveValue sobre un json.RawMessage completo
// (usado para properties/sku/identity) y lo vuelve a serializar.
func resolveJSON(raw json.RawMessage, ctx *evalContext) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	resolved, err := resolveValue(v, ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(resolved)
}

// resolveString resuelve una expresión ARM forzando el resultado a
// string — usado para campos que ARM siempre tipa como string (name,
// location, type de dependsOn) aunque la expresión técnicamente pudiera
// evaluar a otra cosa.
func resolveString(s string, ctx *evalContext) (string, error) {
	v, err := evalExpression(s, ctx)
	if err != nil {
		return "", err
	}
	switch t := v.(type) {
	case string:
		return t, nil
	default:
		return fmt.Sprint(t), nil
	}
}

// evalExpression evalúa un string que puede ser un literal o una
// expresión ARM completa envuelta en corchetes (`"[funcCall(...)]"`).
// `"[["` al inicio es el escape estándar de ARM para un corchete literal.
func evalExpression(s string, ctx *evalContext) (any, error) {
	if strings.HasPrefix(s, "[[") {
		return s[1:], nil
	}
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return s, nil
	}
	inner := s[1 : len(s)-1]
	p := &exprParser{src: inner}
	val, err := p.parseCall(ctx)
	if err != nil {
		return nil, fmt.Errorf("expresión ARM %q: %w", s, err)
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return nil, fmt.Errorf("expresión ARM %q: contenido inesperado al final", s)
	}
	return val, nil
}

// exprParser es un parser recursivo mínimo para el subconjunto de
// funciones soportado: parameters('x'), variables('x'),
// resourceId([subscriptionId,] [resourceGroupName,] resourceType,
// resourceName1, [resourceName2, ...]). No soporta concat(), format(),
// resourceGroup(), uniqueString() ni ningún otro built-in de ARM — ver
// ROADMAP.md, Phase 19, para el alcance deliberadamente incompleto.
type exprParser struct {
	src string
	pos int
}

func (p *exprParser) skipSpace() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t' || p.src[p.pos] == '\n') {
		p.pos++
	}
}

func (p *exprParser) parseCall(ctx *evalContext) (any, error) {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.src) && isIdentChar(p.src[p.pos]) {
		p.pos++
	}
	name := p.src[start:p.pos]
	if name == "" {
		return nil, fmt.Errorf("se esperaba un nombre de función en la posición %d", start)
	}
	p.skipSpace()
	if p.pos >= len(p.src) || p.src[p.pos] != '(' {
		return nil, fmt.Errorf("se esperaba '(' después de %q", name)
	}
	p.pos++ // consume '('

	var args []any
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] != ')' {
		for {
			arg, err := p.parseArg(ctx)
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			p.skipSpace()
			if p.pos < len(p.src) && p.src[p.pos] == ',' {
				p.pos++
				continue
			}
			break
		}
	}
	p.skipSpace()
	if p.pos >= len(p.src) || p.src[p.pos] != ')' {
		return nil, fmt.Errorf("se esperaba ')' cerrando la llamada a %q", name)
	}
	p.pos++ // consume ')'

	return callFunction(name, args, ctx)
}

func (p *exprParser) parseArg(ctx *evalContext) (any, error) {
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == '\'' {
		return p.parseStringLiteral()
	}
	return p.parseCall(ctx)
}

func (p *exprParser) parseStringLiteral() (string, error) {
	if p.src[p.pos] != '\'' {
		return "", fmt.Errorf("se esperaba un literal de string")
	}
	p.pos++
	var b strings.Builder
	for {
		if p.pos >= len(p.src) {
			return "", fmt.Errorf("literal de string sin cerrar")
		}
		c := p.src[p.pos]
		if c == '\'' {
			// ARM escapa una comilla simple literal como ''.
			if p.pos+1 < len(p.src) && p.src[p.pos+1] == '\'' {
				b.WriteByte('\'')
				p.pos += 2
				continue
			}
			p.pos++
			return b.String(), nil
		}
		b.WriteByte(c)
		p.pos++
	}
}

func isIdentChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func callFunction(name string, args []any, ctx *evalContext) (any, error) {
	switch name {
	case "parameters":
		key, err := argString(args, 0, "parameters")
		if err != nil {
			return nil, err
		}
		v, ok := ctx.parameters[key]
		if !ok {
			return nil, fmt.Errorf("parameters('%s'): parámetro no definido", key)
		}
		return v, nil
	case "variables":
		key, err := argString(args, 0, "variables")
		if err != nil {
			return nil, err
		}
		v, ok := ctx.variables[key]
		if !ok {
			return nil, fmt.Errorf("variables('%s'): variable no definida", key)
		}
		return v, nil
	case "resourceId":
		return resourceIDFunc(args, ctx)
	default:
		return nil, fmt.Errorf("función ARM no soportada: %q (solo parameters()/variables()/resourceId() están implementadas)", name)
	}
}

func argString(args []any, idx int, fn string) (string, error) {
	if idx >= len(args) {
		return "", fmt.Errorf("%s(): falta el argumento %d", fn, idx)
	}
	s, ok := args[idx].(string)
	if !ok {
		return "", fmt.Errorf("%s(): el argumento %d debe ser un string", fn, idx)
	}
	return s, nil
}

// resourceIDFunc replica resourceId() en su forma habitual de 2+
// argumentos (resourceType, resourceName1, [resourceName2, ...]) sin
// overrides explícitos de subscriptionId/resourceGroupName — el caso de
// uso real de cualquier template de un solo resource group. Si alguno de
// los argumentos no es string (p. ej. viene de una llamada anidada que
// evaluó a otra cosa), se convierte con fmt.Sprint.
func resourceIDFunc(args []any, ctx *evalContext) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("resourceId(): se esperan al menos 2 argumentos (resourceType, resourceName)")
	}
	strs := make([]string, len(args))
	for i, a := range args {
		if s, ok := a.(string); ok {
			strs[i] = s
		} else {
			strs[i] = fmt.Sprint(a)
		}
	}
	resourceType := strs[0]
	nameParts := strs[1:]

	slash := strings.Index(resourceType, "/")
	if slash < 0 {
		return nil, fmt.Errorf("resourceId(): el resourceType %q debe tener la forma 'Namespace/type[/subtype...]'", resourceType)
	}
	namespace := resourceType[:slash]
	typeSegments := strings.Split(resourceType[slash+1:], "/")
	if len(typeSegments) != len(nameParts) {
		return nil, fmt.Errorf("resourceId(): %d segmentos de tipo pero %d de nombre en %q", len(typeSegments), len(nameParts), resourceType)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "/subscriptions/%s/resourceGroups/%s/providers/%s", ctx.subID, ctx.rg, namespace)
	for i, seg := range typeSegments {
		fmt.Fprintf(&b, "/%s/%s", seg, nameParts[i])
	}
	return b.String(), nil
}

// resourcePath construye el path ARM (sin host ni api-version) bajo el
// que vive un recurso de template.resources[*], a partir de su Type ya
// resuelto y su Name ya resuelto — exactamente el mismo shape que
// resourceIDFunc produce, así que dependsOn (que normalmente contiene un
// resourceId(...) apuntando a otro recurso del mismo template) puede
// compararse directamente contra esto.
func resourcePath(subID, rg, resType, name string) (string, error) {
	slash := strings.Index(resType, "/")
	if slash < 0 {
		return "", fmt.Errorf("type %q debe tener la forma 'Namespace/type[/subtype...]'", resType)
	}
	namespace := resType[:slash]
	typeSegments := strings.Split(resType[slash+1:], "/")
	nameParts := strings.Split(name, "/")
	if len(typeSegments) != len(nameParts) {
		return "", fmt.Errorf("type %q tiene %d segmentos pero name %q tiene %d", resType, len(typeSegments), name, len(nameParts))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "/subscriptions/%s/resourceGroups/%s/providers/%s", subID, rg, namespace)
	for i, seg := range typeSegments {
		fmt.Fprintf(&b, "/%s/%s", seg, nameParts[i])
	}
	return b.String(), nil
}

// resolvedResource es una templateResource después de resolver todas sus
// expresiones ARM, lista para despachar como un PUT real contra el mux
// del emulador.
type resolvedResource struct {
	original   templateResource
	resType    string
	name       string
	location   string
	properties json.RawMessage
	sku        json.RawMessage
	identity   json.RawMessage
	path       string // path ARM completo, ej. /subscriptions/.../providers/Microsoft.Storage/storageAccounts/foo
}

// resolveResources resuelve name/location/properties/sku/identity de cada
// recurso del template (en el orden en que aparecen) y calcula su path
// ARM. No aplica todavía el ordenamiento por dependsOn — eso lo hace
// orderByDependencies sobre el resultado de esta función.
func resolveResources(tmpl *template, ctx *evalContext) ([]resolvedResource, error) {
	out := make([]resolvedResource, 0, len(tmpl.Resources))
	for i, r := range tmpl.Resources {
		name, err := resolveString(r.Name, ctx)
		if err != nil {
			return nil, fmt.Errorf("resources[%d] (%s): name: %w", i, r.Type, err)
		}
		location := r.Location
		if location != "" {
			location, err = resolveString(location, ctx)
			if err != nil {
				return nil, fmt.Errorf("resources[%d] (%s): location: %w", i, r.Type, err)
			}
		}
		props, err := resolveJSON(r.Properties, ctx)
		if err != nil {
			return nil, fmt.Errorf("resources[%d] (%s): properties: %w", i, r.Type, err)
		}
		sku, err := resolveJSON(r.SKU, ctx)
		if err != nil {
			return nil, fmt.Errorf("resources[%d] (%s): sku: %w", i, r.Type, err)
		}
		identity, err := resolveJSON(r.Identity, ctx)
		if err != nil {
			return nil, fmt.Errorf("resources[%d] (%s): identity: %w", i, r.Type, err)
		}
		path, err := resourcePath(ctx.subID, ctx.rg, r.Type, name)
		if err != nil {
			return nil, fmt.Errorf("resources[%d] (%s): %w", i, r.Type, err)
		}
		out = append(out, resolvedResource{
			original:   r,
			resType:    r.Type,
			name:       name,
			location:   location,
			properties: props,
			sku:        sku,
			identity:   identity,
			path:       path,
		})
	}
	return out, nil
}

// orderByDependencies ordena resources según dependsOn (orden topológico,
// Kahn) para que, por ejemplo, una virtual network se cree antes que la
// subnet/NIC que depende de ella. Cada entrada de dependsOn se evalúa
// como expresión ARM (típicamente resourceId(...)) y se compara contra
// el .path ya calculado de cada recurso; si una entrada no evalúa a un
// path conocido del template (porque apunta a un recurso ya existente
// fuera de este deployment, o porque usa una función no soportada), se
// ignora silenciosamente en vez de fallar todo el deployment — el
// ordenamiento es un best-effort, no una validación estricta.
func orderByDependencies(resources []resolvedResource, ctx *evalContext) ([]resolvedResource, error) {
	n := len(resources)
	pathToIdx := make(map[string]int, n)
	for i, r := range resources {
		pathToIdx[r.path] = i
	}

	deps := make([][]int, n)
	for i, r := range resources {
		for _, raw := range r.original.DependsOn {
			v, err := evalExpression(raw, ctx)
			if err != nil {
				continue // best-effort, ver doc del comentario de la función
			}
			s, ok := v.(string)
			if !ok {
				continue
			}
			if j, found := pathToIdx[s]; found && j != i {
				deps[i] = append(deps[i], j)
			}
		}
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make([]int, n)
	order := make([]int, 0, n)
	var visit func(i int) error
	visit = func(i int) error {
		switch color[i] {
		case black:
			return nil
		case gray:
			return fmt.Errorf("ciclo de dependencias detectado en dependsOn (involucrando %s/%s)", resources[i].resType, resources[i].name)
		}
		color[i] = gray
		for _, j := range deps[i] {
			if err := visit(j); err != nil {
				return err
			}
		}
		color[i] = black
		order = append(order, i)
		return nil
	}
	for i := 0; i < n; i++ {
		if err := visit(i); err != nil {
			return nil, err
		}
	}

	ordered := make([]resolvedResource, n)
	for pos, i := range order {
		ordered[pos] = resources[i]
	}
	return ordered, nil
}
