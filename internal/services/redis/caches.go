package redis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const redisBucket = "redis.caches"

// RedisCache replica el subconjunto relevante de Microsoft.Cache/redis
// que az/Terraform (azurerm_redis_cache) leen/escriben.
//
// Nota de estructura: go-azure-sdk (azurerm provider) coloca el SKU dentro de
// "properties" tanto en el PUT (RedisCreateParameters.Properties.Sku) como en
// el GET (RedisResource.Properties.Sku). Por eso RedisCache NO lleva un campo
// Sku de nivel superior — el SKU viaja exclusivamente dentro de Properties.
type RedisCache struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Type       string               `json:"type"`
	Location   string               `json:"location"`
	Tags       map[string]string    `json:"tags,omitempty"`
	Properties RedisCacheProperties `json:"properties"`
}

// RedisSku representa el SKU de Redis Cache.
type RedisSku struct {
	Name     string `json:"name"`
	Family   string `json:"family"`
	Capacity int    `json:"capacity"`
}

// RedisCacheProperties son las propiedades ARM devueltas en GET/PUT.
//
// Sku vive aquí (no en el nivel superior de RedisCache) porque go-azure-sdk
// modela RedisResource.Properties.Sku — tanto para la lectura (GET) como para
// la escritura (PUT RedisCreateParameters.Properties.Sku).
type RedisCacheProperties struct {
	// Sku dentro de properties: requerido por go-azure-sdk / azurerm provider.
	Sku                 RedisSku `json:"sku"`
	HostName            string   `json:"hostName"`
	Port                int      `json:"port"`
	SslPort             int      `json:"sslPort"`
	ProvisioningState   string   `json:"provisioningState"`
	RedisVersion        string   `json:"redisVersion"`
	EnableNonSslPort    bool     `json:"enableNonSslPort"`
	MinimumTLSVersion   string   `json:"minimumTlsVersion"`
	PublicNetworkAccess string   `json:"publicNetworkAccess"`
	// RedisConfiguration debe ser un objeto presente (no null ni ausente)
	// porque azurerm_redis_cache.Read lo indexa directamente sin nil-check
	// ("flattenRedisConfiguration(props.RedisConfiguration)"). Un objeto
	// vacío es suficiente para evitar el panic.
	RedisConfiguration map[string]any `json:"redisConfiguration"`
	// AccessKeys se incluye embebido en el GET para que el provider pueda
	// leer primaryKey/secondaryKey sin necesitar una llamada a listKeys
	// separada en flujos de solo lectura.
	AccessKeys *RedisAccessKeys `json:"accessKeys,omitempty"`
}

// RedisAccessKeys son las claves de acceso fake devueltas por listKeys y
// embebidas opcionalmente en el GET.
type RedisAccessKeys struct {
	PrimaryKey   string `json:"primaryKey"`
	SecondaryKey string `json:"secondaryKey"`
}

type redisCacheRequest struct {
	Location string            `json:"location"`
	Tags     map[string]string `json:"tags,omitempty"`
	// Sku de nivel superior: usado por az CLI (az rest --body '{"sku":{...}}').
	Sku *RedisSku `json:"sku,omitempty"`
	// Properties: formato que usa go-azure-sdk / azurerm provider.
	// El SKU viaja como properties.sku en RedisCreateParameters.Properties.Sku.
	Properties struct {
		// Sku dentro de properties: formato del provider (go-azure-sdk).
		Sku               *RedisSku `json:"sku,omitempty"`
		EnableNonSslPort  bool      `json:"enableNonSslPort"`
		MinimumTLSVersion string    `json:"minimumTlsVersion"`
		RedisVersion      string    `json:"redisVersion"`
	} `json:"properties"`
}

func redisKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + name
}

// fakeHostName devuelve el FQDN determinista de la instancia.
func fakeHostName(name string) string {
	return name + ".redis.cache.windows.net"
}

// fakeKey genera una clave de acceso determinista de 44 caracteres
// derivada de name + seed (mismo patrón que containerregistry.listCredentials).
func fakeKey(name, seed string) string {
	h := sha256.Sum256([]byte(name + ":" + seed))
	return hex.EncodeToString(h[:])[:44]
}

func skuOrDefault(req *redisCacheRequest) RedisSku {
	// Prioridad 1: properties.sku (formato go-azure-sdk / azurerm provider).
	if req.Properties.Sku != nil && req.Properties.Sku.Name != "" {
		return *req.Properties.Sku
	}
	// Prioridad 2: sku de nivel superior (formato az CLI / requests manuales).
	if req.Sku != nil && req.Sku.Name != "" {
		return *req.Sku
	}
	return RedisSku{Name: "Basic", Family: "C", Capacity: 0}
}

func skuFamily(name string) string {
	switch name {
	case "Premium":
		return "P"
	default:
		return "C"
	}
}

func (s *Service) registerRedis(mux *http.ServeMux) {
	base := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Cache/redis"
	mux.HandleFunc("GET "+base, s.listCaches)
	mux.HandleFunc("PUT "+base+"/{cacheName}", s.putCache)
	mux.HandleFunc("PATCH "+base+"/{cacheName}", s.putCache) // actualizaciones in-place
	mux.HandleFunc("GET "+base+"/{cacheName}", s.getCacheHandler)
	mux.HandleFunc("DELETE "+base+"/{cacheName}", s.deleteCache)
	mux.HandleFunc("POST "+base+"/{cacheName}/listKeys", s.listKeys)
}

func (s *Service) putCache(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("cacheName")

	var req redisCacheRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "InvalidRequestContent",
			fmt.Sprintf("no se pudo interpretar el cuerpo de la solicitud: %v", err))
		return
	}

	sku := skuOrDefault(&req)
	if sku.Family == "" {
		sku.Family = skuFamily(sku.Name)
	}

	minTLS := req.Properties.MinimumTLSVersion
	if minTLS == "" {
		minTLS = "1.2"
	}
	redisVer := req.Properties.RedisVersion
	if redisVer == "" {
		redisVer = "6"
	}

	id := "/subscriptions/" + subID + "/resourceGroups/" + rg +
		"/providers/Microsoft.Cache/redis/" + name
	keys := &RedisAccessKeys{
		PrimaryKey:   fakeKey(name, "primary"),
		SecondaryKey: fakeKey(name, "secondary"),
	}

	cache := RedisCache{
		ID:       id,
		Name:     name,
		Type:     "Microsoft.Cache/redis",
		Location: req.Location,
		Tags:     req.Tags,
		Properties: RedisCacheProperties{
			// Sku dentro de properties: formato que lee go-azure-sdk / azurerm.
			Sku:                 sku,
			HostName:            fakeHostName(name),
			Port:                6379,
			SslPort:             6380,
			ProvisioningState:   "Succeeded",
			RedisVersion:        redisVer,
			EnableNonSslPort:    req.Properties.EnableNonSslPort,
			MinimumTLSVersion:   minTLS,
			PublicNetworkAccess: "Enabled",
			RedisConfiguration:  map[string]any{},
			AccessKeys:          keys,
		},
	}

	key := redisKey(subID, rg, name)
	_, existedBefore, err := s.getCache(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if err := s.db.Put(redisBucket, key, cache); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	status := http.StatusCreated
	if existedBefore {
		status = http.StatusOK
	}
	server.WriteJSON(w, status, cache)
}

func (s *Service) getCache(subID, rg, name string) (RedisCache, bool, error) {
	var c RedisCache
	found, err := s.db.Get(redisBucket, redisKey(subID, rg, name), &c)
	return c, found, err
}

func (s *Service) getCacheHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("cacheName")

	c, found, err := s.getCache(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la instancia de Redis '%s' no existe en el resource group '%s'", name, rg))
		return
	}
	server.WriteJSON(w, http.StatusOK, c)
}

func (s *Service) listCaches(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")

	caches := make([]RedisCache, 0)
	err := s.db.List(redisBucket, subID+"/"+rg+"/", func(_ string, raw []byte) error {
		var c RedisCache
		if err := json.Unmarshal(raw, &c); err != nil {
			return err
		}
		caches = append(caches, c)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": caches})
}

func (s *Service) deleteCache(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("cacheName")
	key := redisKey(subID, rg, name)

	found, err := s.db.Get(redisBucket, key, &RedisCache{})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.db.Delete(redisBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// listKeys devuelve las claves de acceso fake de la instancia.
// El provider azurerm_redis_cache llama a este endpoint en su Read para
// obtener primaryKey/secondaryKey/primaryConnectionString.
func (s *Service) listKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("cacheName")

	c, found, err := s.getCache(subID, rg, name)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("la instancia de Redis '%s' no existe en el resource group '%s'", name, rg))
		return
	}

	primary := fakeKey(name, "primary")
	secondary := fakeKey(name, "secondary")
	host := c.Properties.HostName

	server.WriteJSON(w, http.StatusOK, map[string]any{
		"primaryKey":   primary,
		"secondaryKey": secondary,
		// primaryConnectionString es lo que azurerm_redis_cache expone como
		// atributo de salida y que otros recursos referencian con
		// azurerm_redis_cache.X.primary_connection_string.
		"primaryConnectionString":   host + ":6380,password=" + primary + ",ssl=True,abortConnect=False",
		"secondaryConnectionString": host + ":6380,password=" + secondary + ",ssl=True,abortConnect=False",
	})
}
