package httpapi

import (
	"net/http"
	"time"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type FreezeRequest struct {
	StartsAt string `json:"starts_at" example:"2026-07-25"`
	EndsAt   string `json:"ends_at" example:"2026-08-05"`
}

// @Summary		Freeze my membership
// @Description	Schedules a freeze: periods overlapping it are not judged. One active or scheduled freeze at a time; a cooldown of max(7, length) days follows each freeze.
// @Tags			rooms
// @Security		BearerAuth
// @Accept			json
// @Produce		json
// @Param			id		path		int				true	"room id"
// @Param			body	body		FreezeRequest	true	"freeze window, dates in YYYY-MM-DD"
// @Success		201		{object}	domain.Freeze
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Failure		403		{object}	ErrorResponse
// @Failure		404		{object}	ErrorResponse
// @Failure		500		{object}	ErrorResponse
// @Router			/rooms/{id}/freeze [post]
func (s *Server) handleCreateFreeze(w http.ResponseWriter, r *http.Request) {
	room, ok := s.membership(w, r)
	if !ok {
		return
	}
	var req FreezeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	starts, err1 := time.Parse("2006-01-02", req.StartsAt)
	ends, err2 := time.Parse("2006-01-02", req.EndsAt)
	if err1 != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "dates must be YYYY-MM-DD")
		return
	}
	userID := userFrom(r.Context()).ID
	history, err := s.freezes.ListByMember(r.Context(), room.ID, userID)
	if err != nil {
		s.internal(w, err)
		return
	}
	if msg := domain.CanFreeze(history, starts, ends, s.now()); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	fz, err := s.freezes.Create(r.Context(), room.ID, userID, starts, ends)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, fz)
}

// @Summary		Cancel my freeze
// @Description	Cancels the scheduled freeze or unfreezes early. The cooldown counts the days actually used.
// @Tags			rooms
// @Security		BearerAuth
// @Param			id	path	int	true	"room id"
// @Success		204
// @Failure		400	{object}	ErrorResponse
// @Failure		401	{object}	ErrorResponse
// @Failure		403	{object}	ErrorResponse
// @Failure		404	{object}	ErrorResponse
// @Failure		500	{object}	ErrorResponse
// @Router			/rooms/{id}/freeze [delete]
func (s *Server) handleCancelFreeze(w http.ResponseWriter, r *http.Request) {
	room, ok := s.membership(w, r)
	if !ok {
		return
	}
	if err := s.freezes.Cancel(r.Context(), room.ID, userFrom(r.Context()).ID, s.now()); err != nil {
		s.mapError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
