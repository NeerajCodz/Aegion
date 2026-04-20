package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/logger"
)

type telemetryProbe struct {
	shutdownCalls int32
}

func (p *telemetryProbe) Shutdown(ctx context.Context) error {
	atomic.AddInt32(&p.shutdownCalls, 1)
	return nil
}

func TestSafeInt32AndRunConfigBranches(t *testing.T) {
	if _, err := safeInt32(-1, "field"); err == nil || !strings.Contains(err.Error(), "must be >= 0") {
		t.Fatalf("safeInt32(negative) err=%v", err)
	}
	if _, err := safeInt32(1<<31, "field"); err == nil || !strings.Contains(err.Error(), "exceeds int32 range") {
		t.Fatalf("safeInt32(overflow) err=%v", err)
	}

	t.Run("run handles max_open_connections validation and shuts down telemetry", func(t *testing.T) {
		cfg := validMainConfig()
		cfg.Database.MaxOpenConns = -1
		deps, _, stderr, _, _, _ := buildRunDeps(cfg)
		probe := &telemetryProbe{}
		deps.newObservability = func(ctx context.Context, cfg *config.Config) (telemetryProvider, error) {
			return probe, nil
		}

		code := run([]string{"serve"}, deps)
		if code != 1 {
			t.Fatalf("expected exit code 1, got %d", code)
		}
		if !strings.Contains(stderr.String(), "database.max_open_connections") {
			t.Fatalf("expected max_open_connections error, got %q", stderr.String())
		}
		if atomic.LoadInt32(&probe.shutdownCalls) != 1 {
			t.Fatalf("expected telemetry shutdown to run once, got %d", probe.shutdownCalls)
		}
	})

	t.Run("run handles max_idle_connections overflow validation", func(t *testing.T) {
		cfg := validMainConfig()
		cfg.Database.MaxIdleConns = 1 << 31
		deps, _, stderr, _, _, _ := buildRunDeps(cfg)
		code := run([]string{"serve"}, deps)
		if code != 1 {
			t.Fatalf("expected exit code 1, got %d", code)
		}
		if !strings.Contains(stderr.String(), "database.max_idle_connections") {
			t.Fatalf("expected max_idle_connections error, got %q", stderr.String())
		}
	})
}

func TestMainNonZeroExitUsesHook(t *testing.T) {
	origExit := exitProcess
	origArgs := os.Args
	t.Cleanup(func() {
		exitProcess = origExit
		os.Args = origArgs
	})

	var exitCode int
	exitProcess = func(code int) { exitCode = code }
	os.Args = []string{"aegion", "unknown-command"}
	main()
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestDefaultMainDepsStartHTTPServerBranches(t *testing.T) {
	origServe := listenAndServeHTTPHook
	origServeTLS := listenAndServeTLSHook
	origFatal := fatalHTTPServerHook
	t.Cleanup(func() {
		listenAndServeHTTPHook = origServe
		listenAndServeTLSHook = origServeTLS
		fatalHTTPServerHook = origFatal
	})

	listenAndServeHTTPHook = func(*http.Server) error { return errors.New("http failed") }
	listenAndServeTLSHook = func(*http.Server, string, string) error { return errors.New("tls failed") }
	var fatalCalls int32
	fatalHTTPServerHook = func(*logger.Logger) *zerolog.Event {
		atomic.AddInt32(&fatalCalls, 1)
		zl := zerolog.New(io.Discard)
		return (&zl).Error()
	}

	deps := defaultMainDeps()
	log := deps.newLogger(logger.Config{Level: "error", Format: "json"})
	cfg := validMainConfig()

	cfg.Server.TLS.Enabled = false
	nonTLS := &http.Server{Addr: "127.0.0.1:notaport"}
	deps.startHTTPServer(cfg, log, nonTLS)

	cfg.Server.TLS.Enabled = true
	tlsSrv := &http.Server{Addr: "127.0.0.1:0"}
	deps.startHTTPServer(cfg, log, tlsSrv)

	deadline := time.Now().Add(500 * time.Millisecond)
	for atomic.LoadInt32(&fatalCalls) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&fatalCalls) < 2 {
		t.Fatalf("expected fatal hook to be invoked for both branches, got %d", fatalCalls)
	}
}
