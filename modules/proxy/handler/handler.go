package handler

import "net/http"

// Handler exposes HTTP routes for the proxy module.
type Handler struct{}

// New creates a new proxy handler.
func New() *Handler {
	return &Handler{}
}

// RegisterRoutes registers module routes on the provided mux.
func (h *Handler) RegisterRoutes(_ *http.ServeMux) {}
