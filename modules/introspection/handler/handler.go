package handler

import "net/http"

// Handler exposes HTTP routes for introspection.
type Handler struct{}

// New creates a new introspection handler.
func New() *Handler {
	return &Handler{}
}

// RegisterRoutes registers module routes on the provided mux.
func (h *Handler) RegisterRoutes(_ *http.ServeMux) {}
