// Package main is the entry point for the Aegion identity platform.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
	"github.com/aegion/aegion/internal/platform/observability"
	"github.com/aegion/aegion/internal/xlog"
)

//go:embed migrations/*.sql
var migrations embed.FS

var (
	version                = "dev"
	buildTime              = "unknown"
	exitProcess            = os.Exit
	listenAndServeHTTPHook = (*http.Server).ListenAndServe
	listenAndServeTLSHook  = (*http.Server).ListenAndServeTLS
	fatalHTTPServerHook    = (*xlog.Logger).Fatal
)

// Command line flags
const defaultShutdownTimeout = 30 * time.Second

// Command line flags
type flags struct {
	configPath      string
	migrateOnly     bool
	showVersion     bool
	adminBootstrap  bool
	enableWorkers   bool
	shutdownTimeout time.Duration
}

type runtimeServer interface {
	Handler() http.Handler
}

type lifecycleManager interface {
	Shutdown(ctx context.Context) error
}

type telemetryProvider interface {
	Shutdown(ctx context.Context) error
}

type mainDeps struct {
	stdout           io.Writer
	stderr           io.Writer
	loadConfig       func(path string) (*config.Config, error)
	validateConfig   func(cfg *config.Config) error
	newLogger        func(cfg xlog.Config) *xlog.Logger
	connectDB        func(ctx context.Context, cfg database.Config) (*database.DB, error)
	newMigrator      func(db *database.DB) migrator
	runModuleMigrate func(ctx context.Context, cfg *config.Config, db *database.DB, configPath string) error
	newWorkerMgr     func(log *xlog.Logger, db *database.DB) *workers.Manager
	newObservability func(ctx context.Context, cfg *config.Config) (telemetryProvider, error)
	newServer        func(ctx context.Context, cfg *ServerConfig) (runtimeServer, error)
	newHTTPServer    func(cfg *config.Config, handler http.Handler) *http.Server
	newLifecycle     func(cfg *LifecycleConfig) lifecycleManager
	newSignalChan    func() chan os.Signal
	notifySignals    func(c chan<- os.Signal, sig ...os.Signal)
	stopSignals      func(c chan<- os.Signal)
	startHTTPServer  func(cfg *config.Config, log *xlog.Logger, httpServer *http.Server)
}

type migrator interface {
	Migrate(ctx context.Context) error
}

func parseFlags() *flags {
	f := &flags{}
	flag.StringVar(&f.configPath, "config", "aegion.yaml", "Path to configuration file")
	flag.BoolVar(&f.migrateOnly, "migrate", false, "Run migrations and exit")
	flag.BoolVar(&f.showVersion, "version", false, "Show version and exit")
	flag.BoolVar(&f.adminBootstrap, "admin-bootstrap", false, "Bootstrap admin user on startup")
	flag.BoolVar(&f.enableWorkers, "workers", true, "Enable background workers")
	flag.DurationVar(&f.shutdownTimeout, "shutdown-timeout", defaultShutdownTimeout, "Graceful shutdown timeout")
	flag.Parse()
	return f
}

func parseFlagsWithArgs(args []string) (*flags, error) {
	fs := flag.NewFlagSet("aegion", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	f := &flags{}
	fs.StringVar(&f.configPath, "config", "aegion.yaml", "Path to configuration file")
	fs.BoolVar(&f.migrateOnly, "migrate", false, "Run migrations and exit")
	fs.BoolVar(&f.showVersion, "version", false, "Show version and exit")
	fs.BoolVar(&f.adminBootstrap, "admin-bootstrap", false, "Bootstrap admin user on startup")
	fs.BoolVar(&f.enableWorkers, "workers", true, "Enable background workers")
	fs.DurationVar(&f.shutdownTimeout, "shutdown-timeout", defaultShutdownTimeout, "Graceful shutdown timeout")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if err := validateShutdownTimeout(f.shutdownTimeout); err != nil {
		return nil, err
	}

	return f, nil
}

func validateShutdownTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("invalid --shutdown-timeout %q: must be greater than 0", timeout)
	}

	return nil
}

func defaultMainDeps() mainDeps {
	return mainDeps{
		stdout: os.Stdout,
		stderr: os.Stderr,
		loadConfig: func(path string) (*config.Config, error) {
			return config.Load(path)
		},
		validateConfig: func(cfg *config.Config) error {
			return cfg.Validate()
		},
		newLogger: xlog.New,
		connectDB: database.Connect,
		newMigrator: func(db *database.DB) migrator {
			return database.NewMigrator(db, migrations, "migrations")
		},
		runModuleMigrate: runEnabledModuleMigrations,
		newWorkerMgr: func(log *xlog.Logger, db *database.DB) *workers.Manager {
			return workers.NewManager(workers.ManagerConfig{
				DB:  db.Pool,
				Log: log,
			})
		},
		newObservability: func(ctx context.Context, cfg *config.Config) (telemetryProvider, error) {
			obsCfg := observability.DefaultConfig()
			obsCfg.ServiceName = "aegion"
			obsCfg.ServiceVersion = version

			return observability.NewProvider(ctx, obsCfg)
		},
		newServer: func(ctx context.Context, cfg *ServerConfig) (runtimeServer, error) {
			s, err := NewServer(ctx, cfg)
			if err != nil {
				return nil, err
			}
			return s, nil
		},
		newHTTPServer: func(cfg *config.Config, handler http.Handler) *http.Server {
			return &http.Server{
				Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
				Handler:      handler,
				ReadTimeout:  cfg.Server.ReadTimeout.Duration(),
				WriteTimeout: cfg.Server.WriteTimeout.Duration(),
				IdleTimeout:  cfg.Server.IdleTimeout.Duration(),
			}
		},
		newLifecycle: func(cfg *LifecycleConfig) lifecycleManager {
			return NewLifecycle(cfg)
		},
		newSignalChan: func() chan os.Signal {
			return make(chan os.Signal, 1)
		},
		notifySignals: signal.Notify,
		stopSignals:   signal.Stop,
		startHTTPServer: func(cfg *config.Config, log *xlog.Logger, httpServer *http.Server) {
			tlsEnabled := cfg.Server.TLS.Enabled
			certFile := cfg.Server.TLS.CertFile
			keyFile := cfg.Server.TLS.KeyFile
			serveHTTP := listenAndServeHTTPHook
			serveTLS := listenAndServeTLSHook
			go func() {
				log.Info("HTTP server listening",
					"addr", httpServer.Addr,
				)

				var err error
				if tlsEnabled {
					err = serveTLS(httpServer, certFile, keyFile)
				} else {
					err = serveHTTP(httpServer)
				}
				if err != nil && err != http.ErrServerClosed {
					fatalHTTPServerHook(log, "HTTP server failed", "error", err)
				}
			}()
		},
	}
}

func main() {
	if code := run(os.Args[1:], defaultMainDeps()); code != 0 {
		exitProcess(code)
	}
}

func run(args []string, deps mainDeps) int {
	normalizedArgs, command, err := normalizeCLIArgs(args)
	if err != nil {
		_, _ = fmt.Fprintf(deps.stderr, "Failed to parse flags: %v\n", err)
		return 1
	}
	if command == "health" {
		return runHealthCommand(deps.stdout, deps.stderr)
	}

	f, err := parseFlagsWithArgs(normalizedArgs)
	if err != nil {
		_, _ = fmt.Fprintf(deps.stderr, "Failed to parse flags: %v\n", err)
		return 1
	}

	if f.showVersion {
		_, _ = fmt.Fprintf(deps.stdout, "Aegion %s (built %s)\n", version, buildTime)
		return 0
	}

	cfg, err := deps.loadConfig(f.configPath)
	if err != nil {
		_, _ = fmt.Fprintf(deps.stderr, "Failed to load configuration: %v\n", err)
		return 1
	}

	if err := deps.validateConfig(cfg); err != nil {
		_, _ = fmt.Fprintf(deps.stderr, "Invalid configuration: %v\n", err)
		return 1
	}

	log := deps.newLogger(xlog.Config{
		Level:            cfg.Log.Level,
		Format:           cfg.Log.Format,
		ServiceName:      "aegion",
		ServiceNamespace: cfg.Log.ServiceNamespace,
		CloudRegion:      cfg.Log.CloudRegion,
		Environment:      os.Getenv("AEGION_ENV"),
		ServiceVersion:   version,
		CommitHash:       os.Getenv("GIT_COMMIT"),
		Developer:        os.Getenv("DEV_NAME"),
		RedactFields:     cfg.Log.RedactFields,
	})

	log.Info("Starting Aegion",
		"version", version,
		"config", f.configPath,
		"workers", f.enableWorkers,
		"admin_bootstrap", f.adminBootstrap,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var telemetry telemetryProvider
	if deps.newObservability != nil {
		telemetry, err = deps.newObservability(ctx, cfg)
		if err != nil {
			_, _ = fmt.Fprintf(deps.stderr, "Warning: observability disabled: %v\n", err)
		} else {
			defer func() {
				if telemetry != nil {
					_ = telemetry.Shutdown(context.Background())
				}
			}()
		}
	}
	maxOpenConns, err := safeInt32(cfg.Database.MaxOpenConns, "database.max_open_connections")
	if err != nil {
		_, _ = fmt.Fprintf(deps.stderr, "Invalid configuration: %v\n", err)
		return 1
	}
	maxIdleConns, err := safeInt32(cfg.Database.MaxIdleConns, "database.max_idle_connections")
	if err != nil {
		_, _ = fmt.Fprintf(deps.stderr, "Invalid configuration: %v\n", err)
		return 1
	}

	db, err := deps.connectDB(ctx, database.Config{
		URL:             cfg.Database.URL,
		MaxOpenConns:    maxOpenConns,
		MaxIdleConns:    maxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime.Duration(),
		ConnMaxIdleTime: cfg.Database.ConnMaxIdleTime.Duration(),
	})
	if err != nil {
		_, _ = fmt.Fprintf(deps.stderr, "Failed to connect to database: %v\n", err)
		return 1
	}
	defer db.Close()
	log.Info("Connected to database")

	migrator := deps.newMigrator(db)
	if err := migrator.Migrate(ctx); err != nil {
		_, _ = fmt.Fprintf(deps.stderr, "Failed to run migrations: %v\n", err)
		return 1
	}
	log.Info("Migrations complete")

	if deps.runModuleMigrate != nil {
		if err := deps.runModuleMigrate(ctx, cfg, db, f.configPath); err != nil {
			_, _ = fmt.Fprintf(deps.stderr, "Failed to run module migrations: %v\n", err)
			return 1
		}
		log.Info("Module migrations complete")
	}

	if f.migrateOnly || cfg.Database.MigrateOnly {
		log.Info("Migrate-only mode, exiting")
		return 0
	}

	var workerMgr *workers.Manager
	if f.enableWorkers {
		workerMgr = deps.newWorkerMgr(log, db)
		log.Info("Worker manager initialized")
	}

	server, err := deps.newServer(ctx, &ServerConfig{
		Config:         cfg,
		ConfigPath:     f.configPath,
		DB:             db,
		Log:            log,
		WorkerManager:  workerMgr,
		AdminBootstrap: f.adminBootstrap,
	})
	if err != nil {
		_, _ = fmt.Fprintf(deps.stderr, "Failed to initialize server: %v\n", err)
		return 1
	}

	if workerMgr != nil {
		workerMgr.Start(ctx)
		log.Info("Background workers started")
	}

	httpServer := deps.newHTTPServer(cfg, server.Handler())
	deps.startHTTPServer(cfg, log, httpServer)

	lifecycle := deps.newLifecycle(&LifecycleConfig{
		Log:           log,
		Server:        asConcreteServer(server),
		HTTPServer:    httpServer,
		WorkerManager: workerMgr,
		Observability: telemetry,
	})

	sigCh := deps.newSignalChan()
	deps.notifySignals(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer deps.stopSignals(sigCh)
	sig := <-sigCh

	log.Info("Shutdown signal received", "signal", sig.String())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), f.shutdownTimeout)
	defer shutdownCancel()

	if err := lifecycle.Shutdown(shutdownCtx); err != nil {
		telemetry = nil
		log.Error("Error during shutdown", "error", err)
		_, _ = fmt.Fprintf(deps.stderr, "Error during shutdown: %v\n", err)
		return 1
	}
	telemetry = nil

	log.Info("Shutdown complete")
	return 0
}

func normalizeCLIArgs(args []string) ([]string, string, error) {
	if len(args) == 0 {
		return args, "", nil
	}

	command := strings.ToLower(strings.TrimSpace(args[0]))
	if command == "" || strings.HasPrefix(command, "-") {
		return args, "", nil
	}

	switch command {
	case "serve":
		return args[1:], command, nil
	case "migrate":
		return append([]string{"-migrate"}, args[1:]...), command, nil
	case "version":
		return append([]string{"-version"}, args[1:]...), command, nil
	case "health":
		return args[1:], command, nil
	default:
		return nil, "", fmt.Errorf("unknown command: %s", command)
	}
}

func runHealthCommand(stdout, stderr io.Writer) int {
	port := strings.TrimSpace(os.Getenv("AEGION_PORT"))
	if port == "" {
		port = "8080"
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		_, _ = fmt.Fprintf(stderr, "Health check failed: invalid AEGION_PORT %q\n", port)
		return 1
	}

	timeout := strings.TrimSpace(os.Getenv("AEGION_HEALTH_TIMEOUT"))
	timeoutDuration := 2 * time.Second
	if timeout != "" {
		if parsed, err := time.ParseDuration(timeout); err == nil && parsed > 0 {
			timeoutDuration = parsed
		}
	}

	url := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(portNum)) + "/health"
	client := &http.Client{Timeout: timeoutDuration}
	// #nosec G704 -- URL is constrained to localhost and a validated numeric port.
	resp, err := client.Get(url)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Health check failed: %v\n", err)
		return 1
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = fmt.Fprintf(stderr, "Health check failed: status %d\n", resp.StatusCode)
		return 1
	}

	_, _ = fmt.Fprintln(stdout, "ok")
	return 0
}

func asConcreteServer(s runtimeServer) *Server {
	if live, ok := s.(*Server); ok {
		return live
	}
	// Test doubles can still drive lifecycle with a minimal shell server.
	return &Server{}
}

func safeInt32(value int, field string) (int32, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s must be >= 0", field)
	}
	const maxInt32 = int64(1<<31 - 1)
	if int64(value) > maxInt32 {
		return 0, fmt.Errorf("%s exceeds int32 range", field)
	}
	return int32(value), nil
}
