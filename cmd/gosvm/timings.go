package main

import (
	"encoding/json"
	"io"
	"time"
)

// Optional developer diagnostics. Wall time measured by an external caller also
// includes process startup, argument parsing/discovery, and process teardown.
type commandTimings struct {
	Enabled bool               `json:"-"`
	Schema  int                `json:"schema"`
	Command string             `json:"command"`
	Passed  bool               `json:"passed"`
	Phases  map[string]float64 `json:"phase_seconds"`
	Total   float64            `json:"total_seconds"`
}

func (t *commandTimings) measure(name string, operation func() error) error {
	if !t.Enabled {
		return operation()
	}
	start := time.Now()
	err := operation()
	t.Phases[name] = time.Since(start).Seconds()
	return err
}

func (t *commandTimings) emit(out io.Writer, start time.Time) {
	t.Total = time.Since(start).Seconds()
	data, _ := json.Marshal(t)
	out.Write(append(append([]byte("gosvm timings: "), data...), '\n'))
}
