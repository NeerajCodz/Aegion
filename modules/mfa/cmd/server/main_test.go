package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

func TestDefaultListenAddr(t *testing.T) {
	t.Setenv(listenAddrEnv, "")
	if got := defaultListenAddr(); got != defaultListen {
		t.Fatalf("expected default listen addr %q, got %q", defaultListen, got)
	}

	t.Setenv(listenAddrEnv, "127.0.0.1:19003")
	if got := defaultListenAddr(); got != "127.0.0.1:19003" {
		t.Fatalf("expected env listen addr override, got %q", got)
	}
}

func TestModuleConfig(t *testing.T) {
	runtime := &moduleRuntime{
		registerHTTPRoutes: func(*http.ServeMux) {},
		readiness:          func(context.Context) error { return nil },
	}
	cfg := moduleConfig("127.0.0.1:9003", runtime)
	if cfg.Module != "mfa" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9003" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if got, want := strings.Join(cfg.Capabilities, ","), "totp,backup_codes,trusted_devices"; got != want {
		t.Fatalf("unexpected capabilities: %q", got)
	}
	if got, want := strings.Join(cfg.Routes, ","), "/api/v1/mfa/totp/start,/api/v1/mfa/totp/finish,/api/v1/mfa/totp/verify,/api/v1/mfa/backup/verify,/api/v1/mfa/backup/regenerate,/api/v1/mfa/trusted-device"; got != want {
		t.Fatalf("unexpected routes: %q", got)
	}
	if cfg.RegisterHTTPRoutes == nil || cfg.Readiness == nil {
		t.Fatal("module config must expose the installed routes and readiness check")
	}
	if len(cfg.GRPCServices) != 0 || len(cfg.EventSubscriptions) != 0 {
		t.Fatalf("metadata advertises unimplemented services or subscriptions: %+v", cfg)
	}
}

func TestMainInvokesRunModuleServer(t *testing.T) {
	origRun := runModuleServer
	origBuild := buildRuntimeHook
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	t.Cleanup(func() {
		runModuleServer = origRun
		buildRuntimeHook = origBuild
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	})

	buildRuntimeHook = func(context.Context) (*moduleRuntime, error) {
		return &moduleRuntime{cleanup: func() {}}, nil
	}
	var captured moduleserver.Config
	runModuleServer = func(cfg moduleserver.Config) error {
		captured = cfg
		return nil
	}

	os.Args = []string{"mfa-server", "-listen", "127.0.0.1:19003"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()

	if captured.Module != "mfa" || captured.ListenAddr != "127.0.0.1:19003" {
		t.Fatalf("main did not pass expected config: %+v", captured)
	}
}

func TestMainLogsFatalOnRunModuleError(t *testing.T) {
	origRun := runModuleServer
	origBuild := buildRuntimeHook
	origFatal := logFatal
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	t.Cleanup(func() {
		runModuleServer = origRun
		buildRuntimeHook = origBuild
		logFatal = origFatal
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	})

	logFatal = func(v ...any) { panic(v[0]) }
	buildRuntimeHook = func(context.Context) (*moduleRuntime, error) {
		return &moduleRuntime{cleanup: func() {}}, nil
	}
	runModuleServer = func(moduleserver.Config) error { return errors.New("module failed") }
	os.Args = []string{"mfa-server"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected fatal panic")
		}
		got, ok := r.(error)
		if !ok || !strings.Contains(got.Error(), "module failed") {
			t.Fatalf("fatal panic=%v", r)
		}
	}()
	main()
}

func TestDeriveCipherKeyRequiresConfiguredSecret(t *testing.T) {
	t.Setenv(cipherSecretEnv, "")
	t.Setenv(legacyCipherSecretEnv, "")
	if _, err := deriveCipherKey(); err == nil {
		t.Fatal("deriveCipherKey accepted an empty secret")
	}

	t.Setenv(cipherSecretEnv, "mfa-cipher-secret")
	key, err := deriveCipherKey()
	if err != nil {
		t.Fatalf("deriveCipherKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("unexpected cipher key length %d", len(key))
	}
}

func TestReadIdentitySigningSecretRequiresMountedFile(t *testing.T) {
	t.Setenv(identitySigningSecretFileEnv, "")
	if _, err := readIdentitySigningSecret(); err == nil {
		t.Fatal("readIdentitySigningSecret accepted an absent file")
	}

	secretFile := filepath.Join(t.TempDir(), "identity-signing-secret")
	if err := os.WriteFile(secretFile, []byte("12345678901234567890123456789012\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
	t.Setenv(identitySigningSecretFileEnv, secretFile)
	secret, err := readIdentitySigningSecret()
	if err != nil {
		t.Fatalf("readIdentitySigningSecret: %v", err)
	}
	if got, want := string(secret), "12345678901234567890123456789012"; got != want {
		t.Fatalf("secret = %q, want %q", got, want)
	}
}
