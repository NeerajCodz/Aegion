package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCommandUsesOnlySupportedAdminEndpoints(t *testing.T) {
	identityID := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"

	tests := []struct {
		name string
		args []string
		method string
		endpoint string
	}{
		{name: "status", args: []string{"status"}, method: http.MethodGet, endpoint: "/api/admin/setup/status"},
		{name: "list identities", args: []string{"identities", "list", "--page", "2", "--per-page", "25"}, method: http.MethodGet, endpoint: "/api/admin/identities/"},
		{name: "get identity", args: []string{"identities", "get", identityID}, method: http.MethodGet, endpoint: "/api/admin/identities/" + identityID},
		{name: "suspend identity", args: []string{"identities", "suspend", "--yes", identityID}, method: http.MethodPost, endpoint: "/api/admin/identities/" + identityID + "/suspend"},
		{name: "revoke session", args: []string{"sessions", "revoke", "--yes", identityID}, method: http.MethodDelete, endpoint: "/api/admin/sessions/" + identityID},
		{name: "list audit", args: []string{"audit", "list"}, method: http.MethodGet, endpoint: "/api/admin/audit/"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseCommand(test.args)
			if err != nil {
				t.Fatalf("parseCommand(%v) error = %v", test.args, err)
			}
			if got.method != test.method || got.endpoint != test.endpoint {
				t.Fatalf("parseCommand(%v) = %s %s, want %s %s", test.args, got.method, got.endpoint, test.method, test.endpoint)
			}
		})
	}
}

func TestParseCommandRejectsUnsupportedAndUnconfirmedOperations(t *testing.T) {
	identityID := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	for _, args := range [][]string{
		{"execute", "unsupported"},
		{"identities", "delete", identityID},
		{"identities", "suspend", identityID},
		{"sessions", "revoke", identityID},
		{"audit", "delete"},
	} {
		if _, err := parseCommand(args); err == nil {
			t.Fatalf("parseCommand(%v) succeeded unexpectedly", args)
		}
	}
}

func TestEndpointURLAcceptsRootAndAdminBaseURLs(t *testing.T) {
	for _, rawBase := range []string{
		"https://admin.example.test",
		"https://admin.example.test/api/admin",
	} {
		baseURL, err := url.Parse(rawBase)
		if err != nil {
			t.Fatal(err)
		}
		client := &adminClient{baseURL: baseURL}
		endpoint, err := client.endpointURL("/api/admin/setup/status", nil)
		if err != nil {
			t.Fatalf("endpointURL(%q) error = %v", rawBase, err)
		}
		parsed, err := url.Parse(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := parsed.Path, "/api/admin/setup/status"; got != want {
			t.Fatalf("endpointURL(%q) path = %q, want %q", rawBase, got, want)
		}
	}
}

func TestParsePaginationBoundsResults(t *testing.T) {
	query, err := parsePagination([]string{"--page", "3", "--per-page", "40"})
	if err != nil {
		t.Fatalf("parsePagination() error = %v", err)
	}
	if got, want := query.Get("page"), "3"; got != want {
		t.Fatalf("page = %q, want %q", got, want)
	}
	if got, want := query.Get("per_page"), "40"; got != want {
		t.Fatalf("per_page = %q, want %q", got, want)
	}
	if _, err := parsePagination([]string{"--per-page", "101"}); err == nil {
		t.Fatal("parsePagination accepted an unbounded result count")
	}
}

func TestReadAPIKeyRequiresSingleConfiguredKey(t *testing.T) {
	dir := t.TempDir()
	credentialPath := filepath.Join(dir, "credential")
	if err := os.WriteFile(credentialPath, []byte("aegion_abcdefghijkl_valid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := readAPIKey(credentialPath, "aegion_", 12)
	if err != nil {
		t.Fatalf("readAPIKey() error = %v", err)
	}
	if key != "aegion_abcdefghijkl_valid" {
		t.Fatalf("readAPIKey() = %q", key)
	}

	if err := os.WriteFile(credentialPath, []byte("aegion_abcdefghijkl\nanother"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAPIKey(credentialPath, "aegion_", 12); err == nil {
		t.Fatal("readAPIKey accepted multiple credential values")
	}
}

func TestAdminClientSendsCredentialButRedactsResponseSecrets(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer aegion_abcdefghijkl_valid"; got != want {
			t.Errorf("authorization = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Cache-Control"), "no-store"; got != want {
			t.Errorf("cache-control = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/admin/setup/status"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"ready","client_secret":"do-not-print","nested":{"token":"aegion_hidden"}}`)
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	baseURL.Scheme = "https"
	client := &adminClient{baseURL: baseURL, apiKey: "aegion_abcdefghijkl_valid", http: server.Client()}
	response, err := client.do(context.Background(), commandRequest{method: http.MethodGet, endpoint: "/api/admin/setup/status"})
	if err != nil {
		t.Fatalf("client.do() error = %v", err)
	}
	var stdout bytes.Buffer
	if err := writeJSON(&stdout, response); err != nil {
		t.Fatalf("writeJSON() error = %v", err)
	}
	output := stdout.String()
	if strings.Contains(output, "do-not-print") || strings.Contains(output, "aegion_hidden") {
		t.Fatalf("secret reached CLI output: %s", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Fatalf("redacted output missing marker: %s", output)
	}
}

func TestAdminClientBoundsErrorOutput(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"status":"insufficient_permissions","message":"aegion_do_not_print"}}`)
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	baseURL.Scheme = "https"
	client := &adminClient{baseURL: baseURL, apiKey: "aegion_abcdefghijkl_valid", http: server.Client()}
	_, err = client.do(context.Background(), commandRequest{method: http.MethodGet, endpoint: "/api/admin/setup/status"})
	if err == nil {
		t.Fatal("client.do() succeeded unexpectedly")
	}
	if strings.Contains(err.Error(), "do_not_print") {
		t.Fatalf("server error body leaked through CLI error: %v", err)
	}
	var remoteErr *apiError
	if !errorsAs(err, &remoteErr) || remoteErr.status != http.StatusForbidden || remoteErr.code != "insufficient_permissions" {
		t.Fatalf("unexpected API error: %#v", err)
	}
}

func TestRunVersionDoesNotLoadConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := run([]string{"--version"}, &stdout, &stderr); got != exitSuccess {
		t.Fatalf("run(--version) = %d, want %d; stderr=%s", got, exitSuccess, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != artifactName+" "+version {
		t.Fatalf("version output = %q", got)
	}
}

func TestLoadClientConfigFailsClosedWithoutCredentialPaths(t *testing.T) {
	_, err := loadClientConfig(options{
		apiURL:  "https://admin.example.test",
		timeout: time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "--ca-cert-file") {
		t.Fatalf("loadClientConfig() error = %v, want missing CA certificate path", err)
	}
}

func TestWriteJSONBoundedAndRedacted(t *testing.T) {
	var output bytes.Buffer
	value := redactValue(map[string]any{"password": "super-secret", "state": "ready"})
	if err := writeJSON(&output, value); err != nil {
		t.Fatalf("writeJSON() error = %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if decoded["password"] != "[REDACTED]" || decoded["state"] != "ready" {
		t.Fatalf("unexpected output: %#v", decoded)
	}
}

func errorsAs(err error, target any) bool {
	return errors.As(err, target)
}
