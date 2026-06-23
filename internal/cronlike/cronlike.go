// Package cronlike implementa un evaluador mínimo para el shape de
// "recurrence" de los triggers Recurrence de Azure Logic Apps:
// {"frequency": "Hour", "interval": 1, "startTime": "..."} (opcional).
//
// Este es un paquete nuevo, no una reutilización de internal/cronexpr de
// gcp-emulator (5 campos estilo unix-cron: "minuto hora dom mes dow") --
// Logic Apps nunca envía un string de cron: envía frequency+interval, con
// un startTime opcional como ancla. Traducir ese shape a un cron de 5
// campos perdería precisión en los casos con Second/Week/Month (cron
// estándar no soporta segundos ni "cada N semanas/meses" de forma nativa),
// así que este paquete calcula el próximo fire time directamente sobre el
// shape real en vez de pasar por una traducción con pérdida.
package cronlike

import (
	"fmt"
	"strings"
	"time"
)

// Recurrence replica el subconjunto relevante de la propiedad "recurrence"
// de un trigger Recurrence de Logic Apps (definition.triggers.<name>.recurrence).
type Recurrence struct {
	Frequency string `json:"frequency"`
	Interval  int    `json:"interval"`
	StartTime string `json:"startTime,omitempty"` // RFC3339; opcional
}

// Validate comprueba que Frequency sea uno de los valores documentados por
// Azure (Second, Minute, Hour, Day, Week, Month) y que Interval sea
// positivo, igual que rechazaría la API real.
func (r Recurrence) Validate() error {
	switch strings.ToLower(r.Frequency) {
	case "second", "minute", "hour", "day", "week", "month":
	default:
		return fmt.Errorf("cronlike: frequency inválida %q (se esperaba Second/Minute/Hour/Day/Week/Month)", r.Frequency)
	}
	if r.Interval <= 0 {
		return fmt.Errorf("cronlike: interval debe ser > 0, recibido %d", r.Interval)
	}
	if r.StartTime != "" {
		if _, err := time.Parse(time.RFC3339, r.StartTime); err != nil {
			return fmt.Errorf("cronlike: startTime inválido %q: %w", r.StartTime, err)
		}
	}
	return nil
}

// anchor devuelve el punto de referencia desde el cual se cuentan los
// intervalos completos: StartTime si está definido, o `created` (la hora de
// creación/última actualización del workflow) en caso contrario -- sin un
// ancla fija, "cada 2 horas" no tendría un origen estable entre reinicios.
func (r Recurrence) anchor(created time.Time) time.Time {
	if r.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, r.StartTime); err == nil {
			return t
		}
	}
	return created
}

// Next calcula el próximo fire time estrictamente posterior a `after`,
// contando intervalos completos de Frequency*Interval desde el ancla
// (StartTime si está definido, o `created` si no). El contrato es el mismo
// que internal/cronexpr.Schedule.Next en gcp-emulator: estrictamente
// posterior, determinístico, sin estado oculto.
//
// Month es el único caso sin una duración fija en wall-clock (los meses no
// tienen todos la misma longitud); para ese caso se avanza con time.AddDate
// para respetar el calendario real, igual que haría Azure.
func Next(r Recurrence, created, after time.Time) (time.Time, error) {
	if err := r.Validate(); err != nil {
		return time.Time{}, err
	}
	anchor := r.anchor(created)

	if strings.EqualFold(r.Frequency, "month") {
		next := anchor
		for !next.After(after) {
			next = next.AddDate(0, r.Interval, 0)
		}
		return next, nil
	}

	unit, err := unitDuration(r.Frequency)
	if err != nil {
		return time.Time{}, err
	}
	step := unit * time.Duration(r.Interval)

	if anchor.After(after) {
		// El primer disparo (el ancla misma) todavía no ocurrió.
		return anchor, nil
	}
	elapsed := after.Sub(anchor)
	n := elapsed/step + 1
	return anchor.Add(step * n), nil
}

func unitDuration(freq string) (time.Duration, error) {
	switch strings.ToLower(freq) {
	case "second":
		return time.Second, nil
	case "minute":
		return time.Minute, nil
	case "hour":
		return time.Hour, nil
	case "day":
		return 24 * time.Hour, nil
	case "week":
		return 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("cronlike: frequency %q no tiene duración fija", freq)
	}
}
