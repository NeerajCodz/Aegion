package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminhandler "github.com/aegion/aegion/modules/admin/handler"
	"github.com/aegion/aegion/modules/admin/scim"
	adminservice "github.com/aegion/aegion/modules/admin/service"
	adminstore "github.com/aegion/aegion/modules/admin/store"
)

func TestServerAdditionalRoutingAndObservabilityBranches(t *testing.T) {
	t.Run("setupRouter registers SCIM API and mounted routes when enabled", func(t *testing.T) {
		cfg := &Config{}
		cfg.Admin.Path = "/admin"
		cfg.Admin.SCIM.Enabled = true
		cfg.Admin.SCIM.BasePath = "/scim/v2"

		svc := scim.NewService(&serverSCIMStore{}, nil)
		s := &Server{
			Config:      cfg,
			Handler:     adminhandler.New(adminservice.New(adminstore.New(nil), adminservice.Config{})),
			SCIMService: svc,
			SCIMHandler: scim.NewHandler(svc, scim.HandlerConfig{DefaultPageSize: 20, MaxPageSize: 100}),
		}

		r := s.setupRouter()
		if r == nil {
			t.Fatal("setupRouter() returned nil")
		}

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/scim/v2/ServiceProviderConfig", nil))
		if rec.Code == http.StatusNotFound {
			t.Fatalf("expected mounted SCIM route to be registered, got 404")
		}
	})

	t.Run("dashboard observability uses timeout default and invalid URL branch", func(t *testing.T) {
		cfg := &Config{}
		cfg.Observability.Enabled = true
		cfg.Observability.ProbeTimeout = 0
		cfg.Observability.Endpoints.OTelCollector = "://bad-url"
		s := &Server{Config: cfg}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/observability", nil)
		s.handleDashboardObservability(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}

		var probes []dashboardObservabilityProbe
		if err := json.NewDecoder(rec.Body).Decode(&probes); err != nil {
			t.Fatalf("decode probes: %v", err)
		}
		if len(probes) == 0 {
			t.Fatal("expected probes in response")
		}
		if !strings.Contains(strings.ToLower(probes[0].Message), "missing protocol scheme") {
			t.Fatalf("expected invalid URL probe message, got %q", probes[0].Message)
		}
	})
}

func TestServerAdditionalRegisterAndSPAFileBranches(t *testing.T) {
	t.Run("registerWithCore request creation error and request failure", func(t *testing.T) {
		s := &Server{Config: &Config{}}
		s.Config.Admin.Path = "/admin"
		s.Config.Server.Address = "127.0.0.1"
		s.Config.Server.Port = 8082

		s.Config.Core.ServiceURL = "http://[::1"
		if err := s.registerWithCore(context.Background()); err == nil || !strings.Contains(err.Error(), "failed to create registration request") {
			t.Fatalf("registerWithCore(invalid URL) = %v", err)
		}

		s.Config.Core.ServiceURL = "http://127.0.0.1:1"
		if err := s.registerWithCore(context.Background()); err == nil || !strings.Contains(err.Error(), "failed to register with core") {
			t.Fatalf("registerWithCore(request failure) = %v", err)
		}
	})

	t.Run("spa file server root and default cache-control path", func(t *testing.T) {
		spa := NewSPAFileServer()

		rec := httptest.NewRecorder()
		spa.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}

		// Trigger a likely non-js/css/html extension path for default cache control branch.
		rec = httptest.NewRecorder()
		spa.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
		if rec.Code == http.StatusOK && rec.Header().Get("Cache-Control") == "" {
			t.Fatalf("expected Cache-Control for favicon path")
		}
	})
}

func TestProbeDashboardObservabilityRequestCreationBranch(t *testing.T) {
	s := &Server{Config: &Config{}}
	res := s.probeDashboardObservability(context.Background(), dashboardObservabilityEndpoint{
		Key:   "bad",
		Label: "Bad",
		URL:   "://bad-url",
	}, 2*time.Second)
	if res.Status != "offline" || !strings.Contains(strings.ToLower(res.Message), "missing protocol scheme") {
		t.Fatalf("unexpected probe result: %#v", res)
	}
}

