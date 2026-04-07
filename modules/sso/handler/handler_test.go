package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewReturnsDistinctInstances(t *testing.T) {
	first := New()
	second := New()
	if first == nil || second == nil {
		t.Fatal("New returned nil instance")
	}
	if first == second {
		t.Fatal("New returned shared instance")
	}
}

func TestRegisterRoutesPreservesExistingRoutes(t *testing.T) {
	h := New()
	mux := http.NewServeMux()
	mux.HandleFunc("/existing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	h.RegisterRoutes(mux)

	existingReq := httptest.NewRequest(http.MethodGet, "/existing", nil)
	existingRec := httptest.NewRecorder()
	mux.ServeHTTP(existingRec, existingReq)
	if existingRec.Code != http.StatusNoContent {
		t.Fatalf("expected existing route status %d, got %d", http.StatusNoContent, existingRec.Code)
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/not-registered", nil)
	missingRec := httptest.NewRecorder()
	mux.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("expected missing route status %d, got %d", http.StatusNotFound, missingRec.Code)
	}
}
