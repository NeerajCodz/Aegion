package xlog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// JSONSink emits newline-delimited JSON records.
type JSONSink struct {
	w  io.Writer
	mu sync.Mutex
}

// NewJSONSink creates a JSON sink.
func NewJSONSink(w io.Writer) *JSONSink {
	if w == nil {
		w = os.Stdout
	}
	return &JSONSink{w: w}
}

// Emit writes a single JSON record.
func (s *JSONSink) Emit(_ context.Context, r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	enc := json.NewEncoder(s.w)
	return enc.Encode(r.Fields)
}

// MultiSink fans records out to multiple sinks.
type MultiSink struct {
	sinks []Sink
}

// NewMultiSink creates a sink fanout.
func NewMultiSink(sinks ...Sink) *MultiSink {
	kept := make([]Sink, 0, len(sinks))
	for _, sink := range sinks {
		if sink != nil {
			kept = append(kept, sink)
		}
	}
	return &MultiSink{sinks: kept}
}

// Emit sends to every sink and returns a combined error if any sink fails.
func (s *MultiSink) Emit(ctx context.Context, r Record) error {
	var errs []error
	for _, sink := range s.sinks {
		if err := sink.Emit(ctx, r); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("xlog sink errors: %v", errs)
}
