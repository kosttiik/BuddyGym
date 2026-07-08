package httpapi

import (
	"net/http"
	"slices"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

// free themes shipped with the MVP; paid ones come later
var allowedThemes = []string{"default", "dark", "neon"}

type MeResponse struct {
	User         domain.User          `json:"user"`
	Achievements []domain.Achievement `json:"achievements"`
}

type UpdateMeRequest struct {
	Theme string `json:"theme" example:"dark" enums:"default,dark,neon"`
}

// handleGetMe godoc
//
//	@Summary		Get my profile
//	@Description	Returns the authenticated user profile with achievements.
//	@Tags			me
//	@Security		TmaAuth
//	@Produce		json
//	@Success		200	{object}	MeResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/me [get]
func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	achs, err := s.users.Achievements(r.Context(), user.ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	if achs == nil {
		achs = []domain.Achievement{}
	}
	writeJSON(w, http.StatusOK, MeResponse{User: user, Achievements: achs})
}

// handlePatchMe godoc
//
//	@Summary		Update my profile
//	@Description	Changes the profile theme. Status is derived from workouts and cannot be set.
//	@Tags			me
//	@Security		TmaAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		UpdateMeRequest	true	"fields to update"
//	@Success		200		{object}	domain.User
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/me [patch]
func (s *Server) handlePatchMe(w http.ResponseWriter, r *http.Request) {
	var req UpdateMeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !slices.Contains(allowedThemes, req.Theme) {
		writeErr(w, http.StatusBadRequest, "unknown theme")
		return
	}
	user, err := s.users.UpdateTheme(r.Context(), userFrom(r.Context()).ID, req.Theme)
	if err != nil {
		s.mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}
