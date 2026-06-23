package cronlike

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("mustParse(%q): %v", s, err)
	}
	return tm
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		rec     Recurrence
		wantErr bool
	}{
		{"valid hour", Recurrence{Frequency: "Hour", Interval: 1}, false},
		{"valid lowercase", Recurrence{Frequency: "minute", Interval: 5}, false},
		{"valid with startTime", Recurrence{Frequency: "Day", Interval: 1, StartTime: "2026-01-01T00:00:00Z"}, false},
		{"invalid frequency", Recurrence{Frequency: "Fortnight", Interval: 1}, true},
		{"zero interval", Recurrence{Frequency: "Hour", Interval: 0}, true},
		{"negative interval", Recurrence{Frequency: "Hour", Interval: -1}, true},
		{"bad startTime", Recurrence{Frequency: "Hour", Interval: 1, StartTime: "not-a-time"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.rec.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestNextFixedDuration_NoStartTime(t *testing.T) {
	created := mustParse(t, "2026-06-01T00:00:00Z")
	rec := Recurrence{Frequency: "Hour", Interval: 1}

	// after == created: el primer disparo (created+1h) debe ser estrictamente
	// posterior.
	next, err := Next(rec, created, created)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := created.Add(time.Hour)
	if !next.Equal(want) {
		t.Fatalf("Next() = %v, want %v", next, want)
	}

	// after a mitad de un intervalo: debe redondear al siguiente múltiplo.
	mid := created.Add(90 * time.Minute)
	next2, err := Next(rec, created, mid)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want2 := created.Add(2 * time.Hour)
	if !next2.Equal(want2) {
		t.Fatalf("Next() = %v, want %v", next2, want2)
	}
}

func TestNextFixedDuration_FutureStartTime(t *testing.T) {
	created := mustParse(t, "2026-06-01T00:00:00Z")
	future := mustParse(t, "2026-06-10T00:00:00Z")
	rec := Recurrence{Frequency: "Day", Interval: 1, StartTime: future.Format(time.RFC3339)}

	// after antes del ancla: el primer disparo es el ancla mismo.
	next, err := Next(rec, created, created)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !next.Equal(future) {
		t.Fatalf("Next() = %v, want anchor %v", next, future)
	}
}

func TestNextInterval(t *testing.T) {
	created := mustParse(t, "2026-06-01T00:00:00Z")
	rec := Recurrence{Frequency: "Minute", Interval: 15}

	after := created.Add(40 * time.Minute) // entre el 2do (30m) y 3er (45m) tick
	next, err := Next(rec, created, after)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := created.Add(45 * time.Minute)
	if !next.Equal(want) {
		t.Fatalf("Next() = %v, want %v", next, want)
	}
}

func TestNextMonth(t *testing.T) {
	created := mustParse(t, "2026-01-31T00:00:00Z")
	rec := Recurrence{Frequency: "Month", Interval: 1}

	after := mustParse(t, "2026-02-15T00:00:00Z")
	next, err := Next(rec, created, after)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	// time.AddDate(0,1,0) sobre 31 de enero da overflow al 3 de marzo (porque
	// febrero no tiene 31 días) -- documentamos ese comportamiento estándar
	// de Go en vez de intentar "corregirlo" con lógica de fin de mes propia.
	want := created.AddDate(0, 1, 0)
	if !next.Equal(want) {
		t.Fatalf("Next() = %v, want %v", next, want)
	}
}

func TestNextMonthInterval2(t *testing.T) {
	created := mustParse(t, "2026-01-01T00:00:00Z")
	rec := Recurrence{Frequency: "Month", Interval: 2}

	after := mustParse(t, "2026-02-15T00:00:00Z")
	next, err := Next(rec, created, after)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := mustParse(t, "2026-03-01T00:00:00Z")
	if !next.Equal(want) {
		t.Fatalf("Next() = %v, want %v", next, want)
	}
}

func TestNextInvalidPropagatesError(t *testing.T) {
	created := mustParse(t, "2026-06-01T00:00:00Z")
	rec := Recurrence{Frequency: "Fortnight", Interval: 1}
	if _, err := Next(rec, created, created); err == nil {
		t.Fatal("Next() with invalid recurrence: want error, got nil")
	}
}
