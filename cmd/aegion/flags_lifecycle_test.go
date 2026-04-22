package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/aegion/aegion/internal/platform/logger"
)

func withFlagArgs(t *testing.T, args []string, fn func()) {
	t.Helper()

	oldArgs := os.Args
	oldFlagSet := flag.CommandLine

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	flag.CommandLine = fs
	os.Args = args

	defer func() {
		flag.CommandLine = oldFlagSet
		os.Args = oldArgs
	}()

	fn()
}

func testLogger() *logger.Logger {
	return logger.New(logger.Config{
		Level:  "error",
		Format: "json",
	})
}

func TestParseFlagsDefaults(t *testing.T) {
	withFlagArgs(t, []string{"aegion"}, func() {
		f := parseFlags()

		if f.configPath != "aegion.yaml" {
			t.Fatalf("expected default config path aegion.yaml, got %s", f.configPath)
		}
		if f.migrateOnly {
			t.Fatalf("expected default migrateOnly=false")
		}
		if f.showVersion {
			t.Fatalf("expected default showVersion=false")
		}
		if f.adminBootstrap {
			t.Fatalf("expected default adminBootstrap=false")
		}
		if !f.enableWorkers {
			t.Fatalf("expected default enableWorkers=true")
		}
	})
}

func TestParseFlagsCustomValues(t *testing.T) {
	withFlagArgs(t, []string{
		"aegion",
		"-config", "custom.yaml",
		"-migrate",
		"-version",
		"-admin-bootstrap",
		"-workers=false",
	}, func() {
		f := parseFlags()

		if f.configPath != "custom.yaml" {
			t.Fatalf("expected config path custom.yaml, got %s", f.configPath)
		}
		if !f.migrateOnly {
			t.Fatalf("expected migrateOnly=true")
		}
		if !f.showVersion {
			t.Fatalf("expected showVersion=true")
		}
		if !f.adminBootstrap {
			t.Fatalf("expected adminBootstrap=true")
		}
		if f.enableWorkers {
			t.Fatalf("expected enableWorkers=false")
		}
	})
}

func TestDrainMiddleware(t *testing.T) {
	lc := NewLifecycle(&LifecycleConfig{
		Log:        testLogger(),
		Server:     &Server{},
		HTTPServer: &http.Server{},
	})

	handler := lc.DrainMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// Not draining: normal requests pass through.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
	}

	// Draining: normal requests are rejected.
	lc.setDraining(true)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
	if rec.Header().Get("Connection") != "close" {
		t.Fatalf("expected Connection: close while draining")
	}
	if rec.Header().Get("Retry-After") != "30" {
		t.Fatalf("expected Retry-After: 30 while draining")
	}

	// Draining: health checks still pass through.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected health request to pass through with %d, got %d", http.StatusNoContent, rec.Code)
	}
}

func TestShutdownHooksRunOrderAndError(t *testing.T) {
	hooks := NewShutdownHooks()
	order := make([]string, 0, 2)
	hookErr := errors.New("hook failed")

	hooks.Register("first", func(ctx context.Context) error {
		order = append(order, "first")
		return nil
	})
	hooks.Register("second", func(ctx context.Context) error {
		order = append(order, "second")
		return hookErr
	})

	err := hooks.Run(context.Background(), testLogger())
	if !errors.Is(err, hookErr) {
		t.Fatalf("expected hook error %v, got %v", hookErr, err)
	}

	expected := []string{"second", "first"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("expected hook order %v, got %v", expected, order)
	}
}
