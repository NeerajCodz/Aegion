package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	platformjwt "github.com/aegion/aegion/internal/platform/jwt"
	"github.com/aegion/aegion/modules/oauth2/handler"
	"github.com/aegion/aegion/modules/oauth2/service/oidc"
	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestListenAndServeHookDefault(t *testing.T) {
	orig := listenAndServeHook
	srv := &http.Server{Addr: "127.0.0.1:0"}
	_ = srv.Close()
	err := orig(srv)
	if !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("listenAndServeHook default err=%v", err)
	}
}

func TestMainFatalHookBranches(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	origFatal := fatalHook
	origCrypto := cryptoSelfCheckHook
	origLoad := loadConfigHook
	origConnect := connectDBHook
	origBuild := buildHandlerHook
	origServer := newHTTPServerHook
	origNotify := notifySignalsHook
	origStop := stopSignalsHook
	origListen := listenAndServeHook
	origShutdown := shutdownServerHook
	t.Cleanup(func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
		fatalHook = origFatal
		cryptoSelfCheckHook = origCrypto
		loadConfigHook = origLoad
		connectDBHook = origConnect
		buildHandlerHook = origBuild
		newHTTPServerHook = origServer
		notifySignalsHook = origNotify
		stopSignalsHook = origStop
		listenAndServeHook = origListen
		shutdownServerHook = origShutdown
	})

	expectFatal := func(setup func(), want string) {
		t.Helper()
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected fatal panic")
			}
			err, ok := r.(error)
			if !ok || !strings.Contains(err.Error(), want) {
				t.Fatalf("fatal panic=%v, want contains %q", r, want)
			}
		}()
		setup()
		main()
	}

	t.Run("crypto self-check error", func(t *testing.T) {
		fatalHook = func(err error, message string) { panic(err) }
		cryptoSelfCheckHook = func() error { return errors.New("crypto failed") }
		os.Args = []string{"oauth2-server"}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		expectFatal(func() {}, "crypto failed")
	})

	t.Run("load config error", func(t *testing.T) {
		fatalHook = func(err error, message string) { panic(err) }
		cryptoSelfCheckHook = func() error { return nil }
		loadConfigHook = func(string) (*Config, error) { return nil, errors.New("load failed") }
		os.Args = []string{"oauth2-server"}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		expectFatal(func() {}, "load failed")
	})

	t.Run("connect db error", func(t *testing.T) {
		fatalHook = func(err error, message string) { panic(err) }
		cryptoSelfCheckHook = func() error { return nil }
		loadConfigHook = func(string) (*Config, error) {
			cfg := &Config{}
			applyDefaults(cfg)
			return cfg, nil
		}
		connectDBHook = func(context.Context, *Config) (*pgxpool.Pool, error) { return nil, errors.New("db failed") }
		os.Args = []string{"oauth2-server"}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		expectFatal(func() {}, "db failed")
	})

	t.Run("server fatal and shutdown error path", func(t *testing.T) {
		pool, err := pgxpool.New(context.Background(), "postgres://demo:demo@127.0.0.1:1/demo?sslmode=disable&connect_timeout=1")
		if err != nil {
			t.Fatalf("new pool: %v", err)
		}
		t.Cleanup(func() { pool.Close() })

		var fatalCalls int32
		fatalHook = func(err error, message string) { atomic.AddInt32(&fatalCalls, 1) }
		cryptoSelfCheckHook = func() error { return nil }
		loadConfigHook = func(string) (*Config, error) {
			cfg := &Config{}
			applyDefaults(cfg)
			return cfg, nil
		}
		connectDBHook = func(context.Context, *Config) (*pgxpool.Pool, error) { return pool, nil }
		buildHandlerHook = func(cfg *Config, oauthStore *store.Store) *handler.OAuth2Handler { return &handler.OAuth2Handler{} }
		newHTTPServerHook = func(cfg *Config, oauthHandler *handler.OAuth2Handler) *http.Server {
			return &http.Server{Addr: "127.0.0.1:0"}
		}
		listenAndServeHook = func(*http.Server) error { return errors.New("serve failed") }
		shutdownServerHook = func(*http.Server, context.Context) error { return errors.New("shutdown failed") }
		notifySignalsHook = func(c chan<- os.Signal, sig ...os.Signal) {
			go func() {
				time.Sleep(50 * time.Millisecond)
				c <- os.Interrupt
			}()
		}
		stopSignalsHook = func(c chan<- os.Signal) {}

		os.Args = []string{"oauth2-server"}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		main()
		deadline := time.Now().Add(time.Second)
		for atomic.LoadInt32(&fatalCalls) == 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if atomic.LoadInt32(&fatalCalls) == 0 {
			t.Fatal("expected fatal hook call for server failure")
		}
	})
}

func TestConnectDBHookedBranches(t *testing.T) {
	origNewPool := newPoolWithConfigHook
	origPing := poolPingHook
	origClose := poolCloseHook
	t.Cleanup(func() {
		newPoolWithConfigHook = origNewPool
		poolPingHook = origPing
		poolCloseHook = origClose
	})

	cfg := &Config{}
	cfg.Database.URL = "postgres://demo:demo@127.0.0.1:5432/demo?sslmode=disable"
	cfg.Database.MaxConns = 1
	cfg.Database.MinConns = 0

	t.Run("new pool error", func(t *testing.T) {
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) {
			return nil, errors.New("new pool failed")
		}
		_, err := connectDB(context.Background(), cfg)
		if err == nil || err.Error() != "new pool failed" {
			t.Fatalf("connectDB(new pool error) = %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		pool, err := pgxpool.New(context.Background(), "postgres://demo:demo@127.0.0.1:1/demo?sslmode=disable&connect_timeout=1")
		if err != nil {
			t.Fatalf("new pool: %v", err)
		}
		t.Cleanup(func() { pool.Close() })

		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return pool, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		db, err := connectDB(context.Background(), cfg)
		if err != nil || db == nil {
			t.Fatalf("connectDB(success) db=%v err=%v", db, err)
		}
	})
}

func TestOAuth2SigningKeyPairHookedBranches(t *testing.T) {
	origGenerate := generateSigningKeyPairHook
	origValidate := validateSigningKeyPairHook
	t.Cleanup(func() {
		generateSigningKeyPairHook = origGenerate
		validateSigningKeyPairHook = origValidate
	})

	t.Setenv("AEGION_ENV", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("ENV", "")
	t.Setenv(signingPrivateKeyB64Env, "")
	t.Setenv(signingPublicKeyB64Env, "")

	t.Run("generate error", func(t *testing.T) {
		generateSigningKeyPairHook = func(string) (*platformjwt.KeyPair, error) { return nil, errors.New("generate failed") }
		_, err := newOAuth2SigningKeyPair()
		if err == nil || !strings.Contains(err.Error(), "generate signing key pair: generate failed") {
			t.Fatalf("newOAuth2SigningKeyPair(generate error) = %v", err)
		}
	})

	t.Run("defaults algorithm when empty", func(t *testing.T) {
		generateSigningKeyPairHook = func(string) (*platformjwt.KeyPair, error) {
			return &platformjwt.KeyPair{KeyID: "kid-empty"}, nil
		}
		validateSigningKeyPairHook = func(keyPair *platformjwt.KeyPair) error {
			if keyPair.Algorithm != defaultSigningAlgorithm {
				return errors.New("algorithm not defaulted")
			}
			return nil
		}
		kp, err := newOAuth2SigningKeyPair()
		if err != nil || kp == nil || kp.Algorithm != defaultSigningAlgorithm {
			t.Fatalf("newOAuth2SigningKeyPair(default algorithm) kp=%v err=%v", kp, err)
		}
	})

	t.Run("validate error", func(t *testing.T) {
		generateSigningKeyPairHook = func(string) (*platformjwt.KeyPair, error) {
			return &platformjwt.KeyPair{KeyID: "kid", Algorithm: defaultSigningAlgorithm}, nil
		}
		validateSigningKeyPairHook = func(*platformjwt.KeyPair) error { return errors.New("validate failed") }
		_, err := newOAuth2SigningKeyPair()
		if err == nil || err.Error() != "validate failed" {
			t.Fatalf("newOAuth2SigningKeyPair(validate error) = %v", err)
		}
	})
}

func TestStaticJWKSProviderAndBuildHandlerBranches(t *testing.T) {
	origToJWK := toJWKHook
	origNewKP := newOAuth2SigningKeyPairHook
	origNewJWKS := newStaticJWKSProviderHook
	t.Cleanup(func() {
		toJWKHook = origToJWK
		newOAuth2SigningKeyPairHook = origNewKP
		newStaticJWKSProviderHook = origNewJWKS
	})

	keyPair := &platformjwt.KeyPair{
		Algorithm: defaultSigningAlgorithm,
		KeyID:     "kid",
		PublicKey: []byte("pub"),
	}

	t.Run("decode jwk error", func(t *testing.T) {
		toJWKHook = func(string, string, []byte) (string, error) { return "{", nil }
		_, err := newStaticJWKSProvider(keyPair)
		if err == nil || !strings.Contains(err.Error(), "decode jwk") {
			t.Fatalf("newStaticJWKSProvider(decode error) = %v", err)
		}
	})

	t.Run("defaults missing jwk fields", func(t *testing.T) {
		toJWKHook = func(string, string, []byte) (string, error) {
			raw, _ := json.Marshal(oidc.JWK{KTY: "EC"})
			return string(raw), nil
		}
		provider, err := newStaticJWKSProvider(keyPair)
		if err != nil || provider == nil || len(provider.keys) != 1 {
			t.Fatalf("newStaticJWKSProvider(defaults) provider=%v err=%v", provider, err)
		}
		if provider.keys[0].KID != keyPair.KeyID || provider.keys[0].ALG != keyPair.Algorithm || provider.keys[0].USE != "sig" {
			t.Fatalf("unexpected jwk defaults: %+v", provider.keys[0])
		}
	})

	t.Run("build handler panics on signing key init error", func(t *testing.T) {
		newOAuth2SigningKeyPairHook = func() (*platformjwt.KeyPair, error) { return nil, errors.New("signing init failed") }
		defer func() {
			r := recover()
			if r == nil || !strings.Contains(r.(string), "initialize oauth2 signing keys") {
				t.Fatalf("expected signing key panic, got %v", r)
			}
		}()
		cfg := &Config{}
		applyDefaults(cfg)
		buildHandler(cfg, store.NewWithDB(&adapterMockDB{}))
	})

	t.Run("build handler panics on jwks init error", func(t *testing.T) {
		newOAuth2SigningKeyPairHook = func() (*platformjwt.KeyPair, error) {
			return &platformjwt.KeyPair{
				Algorithm:  defaultSigningAlgorithm,
				KeyID:      "kid",
				PrivateKey: []byte("priv"),
				PublicKey:  []byte("pub"),
			}, nil
		}
		newStaticJWKSProviderHook = func(*platformjwt.KeyPair) (*staticJWKSProvider, error) {
			return nil, errors.New("jwks init failed")
		}
		defer func() {
			r := recover()
			if r == nil || !strings.Contains(r.(string), "initialize oauth2 jwks provider") {
				t.Fatalf("expected jwks panic, got %v", r)
			}
		}()
		cfg := &Config{}
		applyDefaults(cfg)
		buildHandler(cfg, store.NewWithDB(&adapterMockDB{}))
	})
}
