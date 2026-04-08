package moduleserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildModuleMuxEndpoints(t *testing.T) {
	cfg := Config{
		Module:             "policy",
		Version:            "1.2.3",
		Capabilities:       []string{"authz", "audit"},
		Routes:             []string{"/v1/check"},
		GRPCServices:       []string{"policy.v1.PolicyService"},
		EventSubscriptions: []string{"identity.updated"},
	}
	handler := buildModuleMux(cfg)

	t.Run("health endpoint", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}

		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode health response: %v", err)
		}
		if body["status"] != "ok" {
			t.Fatalf("expected status=ok, got %q", body["status"])
		}
		if body["module"] != "policy" {
			t.Fatalf("expected module=policy, got %q", body["module"])
		}
	})

	t.Run("ready endpoint", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}

		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode ready response: %v", err)
		}
		if body["status"] != "ready" {
			t.Fatalf("expected status=ready, got %q", body["status"])
		}
		if body["module"] != "policy" {
			t.Fatalf("expected module=policy, got %q", body["module"])
		}
	})

	t.Run("meta endpoint", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/meta", nil)
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}

		var body metaResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode meta response: %v", err)
		}

		if body.Module != "policy" || body.Version != "1.2.3" {
			t.Fatalf("unexpected metadata identity: %#v", body)
		}
		if len(body.Capabilities) != 2 || body.Capabilities[0] != "authz" {
			t.Fatalf("expected capabilities to be preserved, got %#v", body.Capabilities)
		}
		if len(body.Routes) != 1 || body.Routes[0] != "/v1/check" {
			t.Fatalf("expected routes to be preserved, got %#v", body.Routes)
		}
		if len(body.GRPCServices) != 1 || body.GRPCServices[0] != "policy.v1.PolicyService" {
			t.Fatalf("expected gRPC services to be preserved, got %#v", body.GRPCServices)
		}
		if len(body.EventSubscriptions) != 1 || body.EventSubscriptions[0] != "identity.updated" {
			t.Fatalf("expected event subscriptions to be preserved, got %#v", body.EventSubscriptions)
		}
	})
}

func TestRunValidationAndListenErrors(t *testing.T) {
	if err := Run(Config{}); err == nil || !strings.Contains(err.Error(), "module name is required") {
		t.Fatalf("expected missing module validation error, got %v", err)
	}

	err := Run(Config{
		Module:     "policy",
		ListenAddr: "127.0.0.1:not-a-port",
	})
	if err == nil {
		t.Fatalf("expected listen address error")
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("AEGION_MODULE_NAME", "module-from-env")
	if got := EnvOrDefault("AEGION_MODULE_NAME", "fallback-module"); got != "module-from-env" {
		t.Fatalf("expected env value, got %q", got)
	}

	t.Setenv("AEGION_MODULE_NAME", "")
	if got := EnvOrDefault("AEGION_MODULE_NAME", "fallback-module"); got != "fallback-module" {
		t.Fatalf("expected fallback value, got %q", got)
	}
}

func TestRunShutdownViaSignal(t *testing.T) {
	origNotify := signalNotify
	origStop := signalStop
	t.Cleanup(func() {
		signalNotify = origNotify
		signalStop = origStop
	})

	signalNotify = func(c chan<- os.Signal, _ ...os.Signal) {
		go func() {
			c <- os.Interrupt
		}()
	}
	signalStop = func(chan<- os.Signal) {}

	err := Run(Config{
		Module:     "policy",
		ListenAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("expected graceful shutdown, got %v", err)
	}
}

func TestBuildModuleMuxConcurrentRequestLoad(t *testing.T) {
	cfg := Config{
		Module:             "scalable-module",
		Version:            "1.0.0",
		Capabilities:       []string{"health", "ready", "meta"},
		Routes:             []string{"/health", "/ready", "/meta"},
		GRPCServices:       []string{"scalable.v1.Service"},
		EventSubscriptions: []string{"module.updated"},
	}
	server := httptest.NewServer(buildModuleMux(cfg))
	defer server.Close()

	paths := []string{"/health", "/ready", "/meta"}
	client := &http.Client{Timeout: 2 * time.Second}

	const workers = 40
	const requestsPerWorker = 40

	var failures atomic.Int64
	start := time.Now()
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < requestsPerWorker; i++ {
				path := paths[(workerID+i)%len(paths)]
				resp, err := client.Get(server.URL + path)
				if err != nil {
					failures.Add(1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					failures.Add(1)
				}
			}
		}(worker)
	}
	wg.Wait()

	if got := failures.Load(); got != 0 {
		t.Fatalf("expected zero request failures under concurrent load, got %d", got)
	}
	if duration := time.Since(start); duration > 15*time.Second {
		t.Fatalf("expected concurrent load test to complete quickly, took %v", duration)
	}
}
