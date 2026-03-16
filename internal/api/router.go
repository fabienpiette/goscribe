package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/jobs", h.SubmitJob)
	r.Get("/jobs/{id}", h.GetJob)
	r.Get("/actions", h.ListActions)
	r.Get("/health", h.Health)

	return r
}
