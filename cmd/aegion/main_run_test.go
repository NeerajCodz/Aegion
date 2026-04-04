package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
	"github.com/aegion/aegion/internal/platform/logger"
)

type stubMigrator struct {
	err   error
	calls int
}

func (m *stubMigrator) Migrate(ctx context.Context) error {
	m.calls++
	return m.err
}

type stubRuntimeServer struct {
	handler http.Handler
}

func (s *stubRuntimeServer) Handler() http.Handler {
	if s.handler == nil {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	return s.handler
}

type stubLifecycle struct {
	err   error
	calls int
}

func (l *stubLifecycle) Shutdown(ctx context.Context) error {
	l.calls++
	return l.err
}

func validMainConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Database.URL = "postgres://aegion:aegion@localhost:5432/aegion?sslmode=disable"
	cfg.Database.MaxOpenConns = 8
	cfg.Database.MaxIdleConns = 4
	cfg.Database.ConnMaxLifetime = config.Duration(10 * time.Minute)
	cfg.Database.ConnMaxIdleTime = config.Duration(5 * time.Minute)
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 8080
	cfg.Server.ReadTimeout = config.Duration(5 * time.Second)
	cfg.Server.WriteTimeout = config.Duration(5 * time.Second)
	cfg.Server.IdleTimeout = config.Duration(30 * time.Second)
	cfg.Log.Level = "error"
	cfg.Log.Format = "json"
	return cfg
}

func buildRunDeps(cfg *config.Config) (mainDeps, *bytes.Buffer, *bytes.Buffer, *stubMigrator, *stubLifecycle, *stubRuntimeServer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	mig := &stubMigrator{}
	lifecycle := &stubLifecycle{}
	server := &stubRuntimeServer{
		handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	}
	db := &database.DB{}

	deps := mainDeps{
		stdout: stdout,
		stderr: stderr,
		loadConfig: func(path string) (*config.Config, error) {
			return cfg, nil
		},
		validateConfig: func(cfg *config.Config) error {
			return nil
		},
		newLogger: func(cfg logger.Config) *logger.Logger {
			return logger.New(logger.Config{Level: "error", Format: "json"})
		},
		connectDB: func(ctx context.Context, cfg database.Config) (*database.DB, error) {
			return db, nil
		},
		newMigrator: func(db *database.DB) migrator {
			return mig
		},
		newWorkerMgr: func(log *logger.Logger, db *database.DB) *workers.Manager {
			return workers.NewManager(workers.ManagerConfig{Log: log})
		},
		newServer: func(ctx context.Context, cfg *ServerConfig) (runtimeServer, error) {
			return server, nil
		},
		newHTTPServer: func(cfg *config.Config, handler http.Handler) *http.Server {
			return &http.Server{
				Addr:    "127.0.0.1:0",
				Handler: handler,
			}
		},
		newLifecycle: func(cfg *LifecycleConfig) lifecycleManager {
			return lifecycle
		},
		newSignalChan: func() chan os.Signal {
			return make(chan os.Signal, 1)
		},
		notifySignals: func(c chan<- os.Signal, sig ...os.Signal) {
			c <- os.Interrupt
		},
		stopSignals: func(c chan<- os.Signal) {},
		startHTTPServer: func(cfg *config.Config, log *logger.Logger, httpServer *http.Server) {
		},
	}

	return deps, stdout, stderr, mig, lifecycle, server
}

func TestParseFlagsWithArgs(t *testing.T) {
	f, err := parseFlagsWithArgs([]string{"-config", "custom.yaml", "-migrate", "-version", "-admin-bootstrap", "-workers=false"})
	if err != nil {
		t.Fatalf("parseFlagsWithArgs returned error: %v", err)
	}
	if f.configPath != "custom.yaml" || !f.migrateOnly || !f.showVersion || !f.adminBootstrap || f.enableWorkers {
		t.Fatalf("unexpected parsed flags: %+v", f)
	}

	if _, err := parseFlagsWithArgs([]string{"-unknown"}); err == nil {
		t.Fatalf("expected unknown flag error")
	}
}

func TestRunParseFlagError(t *testing.T) {
	deps, _, stderr, _, _, _ := buildRunDeps(validMainConfig())
	if code := run([]string{"-not-a-real-flag"}, deps); code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if got := stderr.String(); !strings.Contains(got, "Failed to parse flags") {
		t.Fatalf("expected parse flag error output, got %q", got)
	}
}

func TestRunVersion(t *testing.T) {
	deps, stdout, _, _, _, _ := buildRunDeps(validMainConfig())
	loadCalled := false
	deps.loadConfig = func(path string) (*config.Config, error) {
		loadCalled = true
		return nil, errors.New("unexpected")
	}

	if code := run([]string{"-version"}, deps); code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if loadCalled {
		t.Fatalf("loadConfig should not be called in version mode")
	}
	if got := strings.TrimSpace(stdout.String()); !strings.Contains(got, "Aegion ") {
		t.Fatalf("expected version output, got %q", got)
	}
}

func TestRunLoadAndValidateErrors(t *testing.T) {
	t.Run("load config error", func(t *testing.T) {
		deps, _, stderr, _, _, _ := buildRunDeps(validMainConfig())
		deps.loadConfig = func(path string) (*config.Config, error) {
			return nil, errors.New("missing file")
		}

		if code := run(nil, deps); code != 1 {
			t.Fatalf("expected exit code 1, got %d", code)
		}
		if got := stderr.String(); !strings.Contains(got, "Failed to load configuration") {
			t.Fatalf("expected load config error, got %q", got)
		}
	})

	t.Run("validate config error", func(t *testing.T) {
		deps, _, stderr, _, _, _ := buildRunDeps(validMainConfig())
		deps.validateConfig = func(cfg *config.Config) error {
			return errors.New("invalid config")
		}

		if code := run(nil, deps); code != 1 {
			t.Fatalf("expected exit code 1, got %d", code)
		}
		if got := stderr.String(); !strings.Contains(got, "Invalid configuration") {
			t.Fatalf("expected validate error, got %q", got)
		}
	})
}

func TestRunDBAndMigrationErrors(t *testing.T) {
	t.Run("db connect error", func(t *testing.T) {
		deps, _, stderr, _, _, _ := buildRunDeps(validMainConfig())
		deps.connectDB = func(ctx context.Context, cfg database.Config) (*database.DB, error) {
			return nil, errors.New("connect failed")
		}

		if code := run(nil, deps); code != 1 {
			t.Fatalf("expected exit code 1, got %d", code)
		}
		if got := stderr.String(); !strings.Contains(got, "Failed to connect to database") {
			t.Fatalf("expected db connect error, got %q", got)
		}
	})

	t.Run("migration error", func(t *testing.T) {
		deps, _, stderr, migrator, _, _ := buildRunDeps(validMainConfig())
		migrator.err = errors.New("migration failed")

		if code := run(nil, deps); code != 1 {
			t.Fatalf("expected exit code 1, got %d", code)
		}
		if migrator.calls != 1 {
			t.Fatalf("expected migrator to be called once, got %d", migrator.calls)
		}
		if got := stderr.String(); !strings.Contains(got, "Failed to run migrations") {
			t.Fatalf("expected migration error output, got %q", got)
		}
	})
}

func TestRunMigrateOnlyPaths(t *testing.T) {
	t.Run("flag migrate only", func(t *testing.T) {
		deps, _, _, migrator, _, _ := buildRunDeps(validMainConfig())
		serverCalled := false
		deps.newServer = func(ctx context.Context, cfg *ServerConfig) (runtimeServer, error) {
			serverCalled = true
			return nil, errors.New("should not be called")
		}

		if code := run([]string{"-migrate"}, deps); code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if migrator.calls != 1 {
			t.Fatalf("expected migrator call, got %d", migrator.calls)
		}
		if serverCalled {
			t.Fatalf("newServer should not be called in migrate-only mode")
		}
	})

	t.Run("config migrate only", func(t *testing.T) {
		cfg := validMainConfig()
		cfg.Database.MigrateOnly = true
		deps, _, _, migrator, _, _ := buildRunDeps(cfg)
		serverCalled := false
		deps.newServer = func(ctx context.Context, cfg *ServerConfig) (runtimeServer, error) {
			serverCalled = true
			return nil, errors.New("should not be called")
		}

		if code := run(nil, deps); code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if migrator.calls != 1 {
			t.Fatalf("expected migrator call, got %d", migrator.calls)
		}
		if serverCalled {
			t.Fatalf("newServer should not be called in config migrate-only mode")
		}
	})
}

func TestRunServerAndShutdownPaths(t *testing.T) {
	t.Run("new server error", func(t *testing.T) {
		deps, _, stderr, _, _, _ := buildRunDeps(validMainConfig())
		deps.newServer = func(ctx context.Context, cfg *ServerConfig) (runtimeServer, error) {
			return nil, errors.New("server init failed")
		}

		if code := run(nil, deps); code != 1 {
			t.Fatalf("expected exit code 1, got %d", code)
		}
		if got := stderr.String(); !strings.Contains(got, "Failed to initialize server") {
			t.Fatalf("expected init server error output, got %q", got)
		}
	})

	t.Run("shutdown error", func(t *testing.T) {
		deps, _, stderr, _, lifecycle, _ := buildRunDeps(validMainConfig())
		lifecycle.err = errors.New("shutdown failed")

		if code := run(nil, deps); code != 1 {
			t.Fatalf("expected exit code 1, got %d", code)
		}
		if lifecycle.calls != 1 {
			t.Fatalf("expected shutdown to be called once, got %d", lifecycle.calls)
		}
		if got := stderr.String(); !strings.Contains(got, "Error during shutdown") {
			t.Fatalf("expected shutdown error output, got %q", got)
		}
	})

	t.Run("success path with workers and admin bootstrap", func(t *testing.T) {
		deps, _, _, migrator, lifecycle, _ := buildRunDeps(validMainConfig())
		workerMgrCalled := false
		startHTTPCalled := false
		stopSignalsCalled := false
		serverCfg := &ServerConfig{}

		deps.newWorkerMgr = func(log *logger.Logger, db *database.DB) *workers.Manager {
			workerMgrCalled = true
			return workers.NewManager(workers.ManagerConfig{Log: log})
		}
		deps.newServer = func(ctx context.Context, cfg *ServerConfig) (runtimeServer, error) {
			serverCfg = cfg
			return &stubRuntimeServer{
				handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			}, nil
		}
		deps.startHTTPServer = func(cfg *config.Config, log *logger.Logger, httpServer *http.Server) {
			startHTTPCalled = true
		}
		deps.stopSignals = func(c chan<- os.Signal) {
			stopSignalsCalled = true
		}
		deps.notifySignals = func(c chan<- os.Signal, sig ...os.Signal) {
			c <- syscall.SIGTERM
		}

		if code := run([]string{"-admin-bootstrap"}, deps); code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if migrator.calls != 1 {
			t.Fatalf("expected migration to run once, got %d", migrator.calls)
		}
		if !workerMgrCalled {
			t.Fatalf("expected worker manager initialization")
		}
		if !startHTTPCalled {
			t.Fatalf("expected HTTP server start hook")
		}
		if !stopSignalsCalled {
			t.Fatalf("expected stopSignals to be called")
		}
		if lifecycle.calls != 1 {
			t.Fatalf("expected lifecycle shutdown once, got %d", lifecycle.calls)
		}
		if !serverCfg.AdminBootstrap {
			t.Fatalf("expected AdminBootstrap=true in server config")
		}
		if serverCfg.WorkerManager == nil {
			t.Fatalf("expected WorkerManager to be passed to server config")
		}
	})
}

func TestAsConcreteServer(t *testing.T) {
	live := &Server{}
	if got := asConcreteServer(&liveServer{server: live}); got != live {
		t.Fatalf("expected live server passthrough")
	}

	fallback := asConcreteServer(&stubRuntimeServer{})
	if fallback == nil {
		t.Fatalf("expected non-nil fallback server")
	}
}

func TestDefaultMainDeps(t *testing.T) {
	deps := defaultMainDeps()
	if deps.stdout == nil || deps.stderr == nil {
		t.Fatalf("expected stdio writers to be initialized")
	}
	if deps.loadConfig == nil || deps.validateConfig == nil || deps.newLogger == nil {
		t.Fatalf("expected core dependency hooks to be initialized")
	}
	if deps.connectDB == nil || deps.newMigrator == nil || deps.newServer == nil {
		t.Fatalf("expected runtime dependency hooks to be initialized")
	}
	if deps.newHTTPServer == nil || deps.newLifecycle == nil || deps.startHTTPServer == nil {
		t.Fatalf("expected server/lifecycle hooks to be initialized")
	}
	if deps.newSignalChan == nil || deps.notifySignals == nil || deps.stopSignals == nil {
		t.Fatalf("expected signal hooks to be initialized")
	}
}
