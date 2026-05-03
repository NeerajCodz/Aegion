package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/cli/service"
	"github.com/aegion/aegion/modules/cli/store"
)

const maxJSONBodyBytes int64 = 1 << 20

type Config struct {
	ManagementToken string
}

type CLIService interface {
	Commands() []service.CommandDescriptor
	Execute(ctx context.Context, req service.ExecuteRequest) (*store.CommandRun, error)
	ListRuns(ctx context.Context, limit int) ([]store.CommandRun, error)
	GetRun(ctx context.Context, id string) (*store.CommandRun, error)
}

type Handler struct {
	svc             CLIService
	managementToken string
}

func New(svc CLIService, cfgOverride ...Config) *Handler {
	cfg := Config{}
	if len(cfgOverride) > 0 {
		cfg = cfgOverride[0]
	}
	return &Handler{
		svc:             svc,
		managementToken: strings.TrimSpace(cfg.ManagementToken),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.HandleFunc("/api/v1/cli/commands", h.handleCommands)
	mux.HandleFunc("/api/v1/cli/runs", h.handleRuns)
	mux.HandleFunc("/api/v1/cli/runs/", h.handleRun)
	mux.HandleFunc("/api/v1/cli/execute", h.handleExecute)
}

func (h *Handler) handleCommands(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": h.svc.Commands()})
}

func (h *Handler) handleRuns(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	runs, err := h.svc.ListRuns(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list command runs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (h *Handler) handleRun(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/v1/cli/runs/"))
	if id == "" {
		writeError(w, http.StatusNotFound, "command run not found")
		return
	}
	run, err := h.svc.GetRun(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "command run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) handleExecute(w http.ResponseWriter, r *http.Request) {
	if !h.requireManagementAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req service.ExecuteRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	run, err := h.svc.Execute(r.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		if !errors.Is(err, service.ErrUnsupportedCommand) && run == nil {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, map[string]any{
			"run": run,
			"error": map[string]any{
				"code":    status,
				"message": err.Error(),
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) requireManagementAuth(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(h.managementToken) == "" {
		writeError(w, http.StatusServiceUnavailable, "cli management is disabled")
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing management token")
		return false
	}
	if !platformcrypto.ConstantTimeCompare([]byte(token), []byte(h.managementToken)) {
		writeError(w, http.StatusUnauthorized, "invalid management token")
		return false
	}
	return true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra struct{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("invalid request body")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    status,
			"message": message,
		},
	})
}
