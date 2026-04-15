package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/aegion/aegion/modules/oauth2/service/revocation"
	tokenservice "github.com/aegion/aegion/modules/oauth2/service/token"
)

const maxJSONBodyBytes int64 = 1 << 20

// IntrospectionService captures the module behavior used by HTTP handlers.
type IntrospectionService interface {
	IntrospectToken(ctx context.Context, req *tokenservice.IntrospectionRequest) (*tokenservice.IntrospectionResponse, error)
}

// Handler exposes HTTP routes for introspection.
type Handler struct {
	svc IntrospectionService
}

// New creates a new introspection handler.
func New(svc IntrospectionService) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers module routes on the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("/oauth2/introspect", h.handleRFC7662Introspection)
	mux.HandleFunc("/api/v1/introspection/token", h.handleJSONIntrospection)
}

func (h *Handler) handleRFC7662Introspection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, "invalid_request", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.svc == nil {
		writeOAuthError(w, "server_error", "Introspection service unavailable", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, "invalid_request", "Failed to parse form", http.StatusBadRequest)
		return
	}
	setNoStoreHeaders(w)

	clientID, clientSecret := extractClientCredentials(r)
	resp, err := h.svc.IntrospectToken(r.Context(), &tokenservice.IntrospectionRequest{
		Token:        r.FormValue("token"),
		ClientID:     clientID,
		ClientSecret: clientSecret,
	})
	if err != nil {
		writeMappedIntrospectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleJSONIntrospection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h == nil || h.svc == nil {
		writeJSONError(w, http.StatusInternalServerError, "introspection service unavailable")
		return
	}

	var req struct {
		Token        string `json:"token"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if basicID, basicSecret, ok := extractBasicAuthCredentials(r); ok {
		req.ClientID = basicID
		req.ClientSecret = basicSecret
	}
	setNoStoreHeaders(w)

	resp, err := h.svc.IntrospectToken(r.Context(), &tokenservice.IntrospectionRequest{
		Token:        req.Token,
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
	})
	if err != nil {
		writeMappedIntrospectionJSONError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeMappedIntrospectionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tokenservice.ErrInvalidClient):
		w.Header().Set("WWW-Authenticate", `Basic realm="oauth2", error="invalid_client"`)
		writeOAuthError(w, "invalid_client", "Client authentication failed", http.StatusUnauthorized)
	case errors.Is(err, tokenservice.ErrInvalidRequest):
		writeOAuthError(w, "invalid_request", "Invalid introspection request", http.StatusBadRequest)
	default:
		writeOAuthError(w, "server_error", "Internal server error", http.StatusInternalServerError)
	}
}

func writeMappedIntrospectionJSONError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tokenservice.ErrInvalidClient):
		w.Header().Set("WWW-Authenticate", `Basic realm="oauth2", error="invalid_client"`)
		writeJSONError(w, http.StatusUnauthorized, "client authentication failed")
	case errors.Is(err, tokenservice.ErrInvalidRequest):
		writeJSONError(w, http.StatusBadRequest, "invalid introspection request")
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
	}
}

func extractClientCredentials(r *http.Request) (clientID, clientSecret string) {
	if r == nil {
		return "", ""
	}
	if clientID, clientSecret, ok := extractBasicAuthCredentials(r); ok {
		return clientID, clientSecret
	}
	return r.FormValue("client_id"), r.FormValue("client_secret")
}

func extractBasicAuthCredentials(r *http.Request) (clientID, clientSecret string, ok bool) {
	if r == nil {
		return "", "", false
	}
	id, secret, err := revocation.ExtractClientCredentials(r.Header.Get("Authorization"))
	if err != nil {
		return "", "", false
	}
	return id, secret, true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra struct{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("invalid request body")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOAuthError(w http.ResponseWriter, code, description string, status int) {
	setNoStoreHeaders(w)
	writeJSON(w, status, map[string]string{
		"error":             code,
		"error_description": description,
	})
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    status,
			"message": strings.TrimSpace(message),
		},
	})
}

func setNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}
