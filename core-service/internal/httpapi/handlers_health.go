package httpapi

import (
	"context"
	"net/http"
	"time"
)

type HealthResponse struct {
	Status   string `json:"status" example:"ok" enums:"ok,degraded"`
	Postgres string `json:"postgres" example:"ok"`
	Redis    string `json:"redis" example:"ok"`
}

// @Summary		Service health
// @Description	Reports core-service health including Postgres and Redis connectivity.
// @Tags			system
// @Produce		json
// @Success		200	{object}	HealthResponse
// @Failure		503	{object}	HealthResponse
// @Router			/health [get]
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	resp := HealthResponse{Status: "ok", Postgres: "ok", Redis: "ok"}
	if err := s.dbPing(ctx); err != nil {
		resp.Status, resp.Postgres = "degraded", err.Error()
	}
	if err := s.redisPing(ctx); err != nil {
		resp.Status, resp.Redis = "degraded", err.Error()
	}
	code := http.StatusOK
	if resp.Status != "ok" {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, resp)
}
