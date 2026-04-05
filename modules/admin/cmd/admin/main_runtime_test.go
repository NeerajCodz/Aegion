package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestDefaultMainDepsProvidesRuntimeHooks(t *testing.T) {
	deps := defaultMainDeps()

	if deps.stdout == nil || deps.loadConfig == nil || deps.setupLogger == nil {
		t.Fatalf("defaultMainDeps returned nil core hooks")
	}
	if deps.parseDBConfig == nil || deps.newDBPool == nil || deps.pingDB == nil || deps.closeDB == nil {
		t.Fatalf("defaultMainDeps returned nil db hooks")
	}
	if deps.runMigrations == nil || deps.startServer == nil || deps.newSignalChan == nil {
		t.Fatalf("defaultMainDeps returned nil runtime hooks")
	}
	if deps.notifySignals == nil || deps.stopSignalChan == nil {
		t.Fatalf("defaultMainDeps returned nil signal hooks")
	}

	deps.closeDB(nil)
	ch := deps.newSignalChan()
	if ch == nil || cap(ch) != 1 {
		t.Fatalf("expected buffered signal channel with cap=1")
	}
}

func TestLiveRuntimeServerAdapters(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Address = "127.0.0.1"
	cfg.Server.Port = 8082

	s := &liveRuntimeServer{
		server: &Server{Config: cfg},
		httpServer: &http.Server{
			Addr: "127.0.0.1:0",
		},
	}

	if err := s.registerWithCore(context.Background()); err != nil {
		t.Fatalf("registerWithCore adapter returned error: %v", err)
	}

	err := s.shutdown(context.Background())
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("shutdown adapter returned unexpected error: %v", err)
	}
}

func TestStartServerRuntimeStartsAndStops(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Address = "127.0.0.1"
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = time.Second
	cfg.Server.WriteTimeout = time.Second
	cfg.Server.IdleTimeout = time.Second
	cfg.Admin.Path = "/admin"

	rt, err := startServerRuntime(cfg, nil)
	if err != nil {
		t.Fatalf("startServerRuntime returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = rt.shutdown(ctx)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("shutdown returned unexpected error: %v", err)
	}
}

func TestMainVersionPath(t *testing.T) {
	origArgs := os.Args
	defer func() {
		os.Args = origArgs
	}()

	os.Args = []string{"aegion-admin", "-version"}
	main()
}
