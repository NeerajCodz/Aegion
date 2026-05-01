package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aegion/aegion/core/orchestrator"
	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
	"github.com/aegion/aegion/internal/platform/logger"
)

type openErrFS struct {
	err error
}

func (f openErrFS) Open(name string) (fs.File, error) {
	return nil, f.err
}

type stubObsProvider struct {
	shutdownCalls int
	shutdownErr   error
}

func (s *stubObsProvider) Shutdown(ctx context.Context) error {
	s.shutdownCalls++
	return s.shutdownErr
}

func TestBootstrapAdminOperatorAdditionalErrorPaths(t *testing.T) {
	origBegin := beginBootstrapAdminTx
	t.Cleanup(func() { beginBootstrapAdminTx = origBegin })

	db := &database.DB{Pool: &pgxpool.Pool{}}

	t.Run("operator count query failure", func(t *testing.T) {
		beginBootstrapAdminTx = func(ctx context.Context, db *database.DB) (pgx.Tx, error) {
			return &adminTestTx{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return adminTestRow{scanFn: func(dest ...any) error { return errors.New("count failed") }}
				},
				rollbackFn: func(ctx context.Context) error { return errors.New("rollback failed") },
			}, nil
		}
		if _, err := bootstrapAdminOperator(context.Background(), db, "admin@example.com", "Password1!"); err == nil {
			t.Fatal("expected operator count query error")
		}
	})

	t.Run("identity lookup non-no-rows error", func(t *testing.T) {
		beginBootstrapAdminTx = func(ctx context.Context, db *database.DB) (pgx.Tx, error) {
			queryCall := 0
			return &adminTestTx{
				queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					queryCall++
					if queryCall == 1 {
						return adminTestRow{scanFn: func(dest ...any) error {
							*(dest[0].(*int)) = 0
							return nil
						}}
					}
					return adminTestRow{scanFn: func(dest ...any) error { return errors.New("identity lookup failed") }}
				},
			}, nil
		}
		if _, err := bootstrapAdminOperator(context.Background(), db, "admin@example.com", "Password1!"); err == nil {
			t.Fatal("expected identity lookup error")
		}
	})

	t.Run("schema resolve and insert failures", func(t *testing.T) {
		tests := []struct {
			name      string
			failOnSQL string
			failErr   error
		}{
			{name: "schema resolve error", failOnSQL: "FROM core_identity_schemas", failErr: errors.New("schema resolve failed")},
			{name: "identity insert error", failOnSQL: "INSERT INTO core_identities", failErr: errors.New("identity insert failed")},
			{name: "address insert error", failOnSQL: "INSERT INTO core_identity_addresses", failErr: errors.New("address insert failed")},
			{name: "credential insert error", failOnSQL: "INSERT INTO pwd_credentials", failErr: errors.New("credential insert failed")},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				beginBootstrapAdminTx = func(ctx context.Context, db *database.DB) (pgx.Tx, error) {
					queryCall := 0
					return &adminTestTx{
						queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
							queryCall++
							switch queryCall {
							case 1:
								return adminTestRow{scanFn: func(dest ...any) error {
									*(dest[0].(*int)) = 0
									return nil
								}}
							case 2:
								return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
							default:
								if strings.Contains(sql, "FROM core_identity_schemas") && strings.Contains(tc.failOnSQL, "core_identity_schemas") {
									return adminTestRow{scanFn: func(dest ...any) error { return tc.failErr }}
								}
								return adminTestRow{scanFn: func(dest ...any) error {
									*(dest[0].(*uuid.UUID)) = uuid.New()
									return nil
								}}
							}
						},
						execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
							if strings.Contains(sql, tc.failOnSQL) {
								return pgconn.CommandTag{}, tc.failErr
							}
							return pgconn.NewCommandTag("INSERT 0 1"), nil
						},
					}, nil
				}
				if _, err := bootstrapAdminOperator(context.Background(), db, "admin@example.com", "Password1!"); err == nil {
					t.Fatalf("expected %s", tc.name)
				}
			})
		}
	})
}

func TestServerAdditionalCoverageBranches(t *testing.T) {
	t.Run("query uses db pool when present", func(t *testing.T) {
		s := newTestServer(t)
		s.db = &database.DB{Pool: newUnreachablePool(t)}

		if _, err := s.query(context.Background(), "SELECT 1"); err == nil {
			t.Fatal("expected query error with unreachable pool")
		}
	})

	t.Run("bootstrap admin no-op outcome", func(t *testing.T) {
		s := newTestServer(t)
		s.cfg.Operator.Email = "admin@example.com"
		s.cfg.Operator.Password = "Password1!"

		origEnsure := ensureBootstrapAdminOperator
		t.Cleanup(func() { ensureBootstrapAdminOperator = origEnsure })
		ensureBootstrapAdminOperator = func(ctx context.Context, db *database.DB, email, password string) (bootstrapAdminOutcome, error) {
			return bootstrapAdminOutcome{}, nil
		}

		if err := s.bootstrapAdmin(context.Background()); err != nil {
			t.Fatalf("bootstrapAdmin should no-op cleanly, got %v", err)
		}
	})

	t.Run("shutdown returns orchestrator stop error", func(t *testing.T) {
		s := newTestServer(t)
		s.orchestrator = &stubModuleOrchestrator{stopErr: errors.New("stop failed")}

		if err := s.Shutdown(context.Background()); err == nil {
			t.Fatal("expected shutdown to return orchestrator stop error")
		}
	})

	t.Run("new server wraps orchestrator constructor error", func(t *testing.T) {
		cfg := &config.Config{
			Server: config.ServerConfig{
				RequestTimeout: config.Duration(5 * time.Second),
				InternalNet: config.InternalNetConfig{
					HealthCheckInt:     config.Duration(time.Second),
					HealthCheckTimeout: config.Duration(time.Second),
				},
			},
			Admin: config.AdminConfig{Path: "/aegion"},
			Secrets: config.SecretsConfig{
				Internal: []string{"dev-internal-secret-change-me-32chars"},
			},
		}

		orig := newModuleOrchestrator
		t.Cleanup(func() { newModuleOrchestrator = orig })
		newModuleOrchestrator = func(cfg orchestrator.Config) (moduleOrchestrator, error) {
			return nil, errors.New("orchestrator ctor failed")
		}

		_, err := NewServer(context.Background(), &ServerConfig{
			Config:     cfg,
			ConfigPath: "configs\\aegion.yaml",
			DB:         &database.DB{Pool: nil},
			Log:        logger.New(logger.Config{Level: "error", Format: "json"}),
		})
		if err == nil || !strings.Contains(err.Error(), "orchestrator ctor failed") {
			t.Fatalf("expected wrapped constructor error, got %v", err)
		}
	})
}

func TestLifecycleAdditionalErrorPaths(t *testing.T) {
	s := newTestServer(t)
	s.orchestrator = &stubModuleOrchestrator{stopErr: errors.New("stop failed")}

	httpSrv := &http.Server{}
	obs := &stubObsProvider{shutdownErr: errors.New("obs shutdown failed")}
	lc := NewLifecycle(&LifecycleConfig{
		Log:           testLogger(),
		Server:        s,
		HTTPServer:    httpSrv,
		Observability: obs,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := lc.Shutdown(ctx)
	if err == nil {
		t.Fatal("expected lifecycle shutdown to return first shutdown error")
	}
	if obs.shutdownCalls != 1 {
		t.Fatalf("expected observability shutdown to be called once, got %d", obs.shutdownCalls)
	}
}

func TestMainAndModuleMigrationCoverageBranches(t *testing.T) {
	t.Run("normalize cli args version branch", func(t *testing.T) {
		args, command, err := normalizeCLIArgs([]string{"version"})
		if err != nil {
			t.Fatalf("normalizeCLIArgs failed: %v", err)
		}
		if command != "version" {
			t.Fatalf("expected command version, got %q", command)
		}
		if len(args) != 1 || args[0] != "-version" {
			t.Fatalf("expected mapped args [-version], got %#v", args)
		}
	})

	t.Run("run health command default and timeout env branches", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		t.Setenv("AEGION_PORT", "")
		t.Setenv("AEGION_HEALTH_TIMEOUT", "")
		if code := runHealthCommand(&stdout, &stderr); code != 1 {
			t.Fatalf("expected failure against default localhost:8080 without server, got %d", code)
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		parsed, err := url.Parse(srv.URL)
		if err != nil {
			t.Fatalf("parse test server url: %v", err)
		}
		_, port, err := net.SplitHostPort(parsed.Host)
		if err != nil {
			t.Fatalf("split host/port: %v", err)
		}
		if _, err := strconv.Atoi(port); err != nil {
			t.Fatalf("invalid parsed port: %v", err)
		}

		stdout.Reset()
		stderr.Reset()
		t.Setenv("AEGION_PORT", port)
		t.Setenv("AEGION_HEALTH_TIMEOUT", "150ms")
		if code := runHealthCommand(&stdout, &stderr); code != 0 {
			t.Fatalf("expected healthy command success, got code %d stderr=%q", code, stderr.String())
		}
		if strings.TrimSpace(stdout.String()) != "ok" {
			t.Fatalf("expected stdout ok, got %q", stdout.String())
		}
	})

	t.Run("default deps observability and newServer error path", func(t *testing.T) {
		deps := defaultMainDeps()
		cfg := validMainConfig()

		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "localhost:4318")
		t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "localhost:4318")
		t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "localhost:4318")

		provider, err := deps.newObservability(context.Background(), cfg)
		if err != nil {
			t.Fatalf("newObservability failed: %v", err)
		}
		if provider != nil {
			_ = provider.Shutdown(context.Background())
		}

		orig := newModuleOrchestrator
		t.Cleanup(func() { newModuleOrchestrator = orig })
		newModuleOrchestrator = func(cfg orchestrator.Config) (moduleOrchestrator, error) {
			return nil, errors.New("forced newServer error")
		}

		cfg.Secrets.Internal = []string{"dev-internal-secret-change-me-32chars"}
		_, err = deps.newServer(context.Background(), &ServerConfig{
			Config:     cfg,
			ConfigPath: "configs\\aegion.yaml",
			DB:         &database.DB{Pool: nil},
			Log:        logger.New(logger.Config{Level: "error", Format: "json"}),
			WorkerManager: workers.NewManager(workers.ManagerConfig{
				Log: logger.New(logger.Config{Level: "error", Format: "json"}),
			}),
		})
		if err == nil || !strings.Contains(err.Error(), "forced newServer error") {
			t.Fatalf("expected forced newServer error, got %v", err)
		}
	})

	t.Run("run logs module migration completion", func(t *testing.T) {
		cfg := validMainConfig()
		deps, _, _, migrator, lifecycle, _ := buildRunDeps(cfg)

		moduleMigrateCalls := 0
		deps.runModuleMigrate = func(ctx context.Context, cfg *config.Config, db *database.DB, configPath string) error {
			moduleMigrateCalls++
			return nil
		}
		if code := run(nil, deps); code != 0 {
			t.Fatalf("expected run success, got %d", code)
		}
		if migrator.calls != 1 {
			t.Fatalf("expected core migrator call, got %d", migrator.calls)
		}
		if moduleMigrateCalls != 1 {
			t.Fatalf("expected module migrator call, got %d", moduleMigrateCalls)
		}
		if lifecycle.calls != 1 {
			t.Fatalf("expected lifecycle shutdown call, got %d", lifecycle.calls)
		}
	})

	t.Run("module migrations helper deps branches", func(t *testing.T) {
		deps := defaultModuleMigrationDeps()
		if deps.moduleOrder == nil || deps.moduleFS == nil || deps.moduleMigrator == nil {
			t.Fatal("expected fully wired default module migration deps")
		}
		if deps.moduleMigrator(&database.DB{}, "oauth2", fstest.MapFS{}, "modules/oauth2/migrations") == nil {
			t.Fatal("expected default module migrator factory to return migrator")
		}

		cfg := &config.Config{ModuleVersions: map[string]string{"oauth2": "latest"}}
		customDeps := moduleMigrationDeps{
			moduleOrder: func(moduleVersions map[string]string) ([]string, error) { return []string{}, nil },
			moduleFS:    func(configPath string) (fs.FS, error) { return fstest.MapFS{}, nil },
			moduleMigrator: func(db *database.DB, moduleID string, migrationFS fs.FS, basePath string) migrator {
				return &moduleTestMigrator{}
			},
		}
		if err := runEnabledModuleMigrationsWithDeps(context.Background(), cfg, &database.DB{}, "configs\\aegion.yaml", customDeps); err != nil {
			t.Fatalf("expected no-op on empty module list, got %v", err)
		}

		customDeps.moduleOrder = func(moduleVersions map[string]string) ([]string, error) { return []string{"oauth2"}, nil }
		customDeps.moduleFS = func(configPath string) (fs.FS, error) { return nil, errors.New("fs load failed") }
		if err := runEnabledModuleMigrationsWithDeps(context.Background(), cfg, &database.DB{}, "configs\\aegion.yaml", customDeps); err == nil {
			t.Fatal("expected module fs load error")
		}

		customDeps.moduleFS = func(configPath string) (fs.FS, error) { return openErrFS{err: errors.New("stat failed")}, nil }
		if err := runEnabledModuleMigrationsWithDeps(context.Background(), cfg, &database.DB{}, "configs\\aegion.yaml", customDeps); err == nil || !strings.Contains(err.Error(), "checking module") {
			t.Fatalf("expected wrapped stat error, got %v", err)
		}
	})
}
