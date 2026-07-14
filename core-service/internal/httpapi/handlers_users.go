package httpapi

import (
	"net/http"
	"strconv"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type UserProfileResponse struct {
	User         domain.User          `json:"user"`
	Achievements []domain.Achievement `json:"achievements"`
	BestStreak   int                  `json:"best_streak"`
}

// handleGetUser godoc
//
//	@Summary		Get a user profile
//	@Description	Public profile of any user by id, with achievements.
//	@Tags			users
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		int	true	"user id"
//	@Success		200	{object}	UserProfileResponse
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/users/{id} [get]
func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	user, err := s.users.Get(r.Context(), id)
	if err != nil {
		s.mapError(w, err)
		return
	}
	achs, err := s.users.Achievements(r.Context(), id)
	if err != nil {
		s.internal(w, err)
		return
	}
	if achs == nil {
		achs = []domain.Achievement{}
	}
	streaks, err := s.streaks.StreaksByUser(r.Context(), id)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, UserProfileResponse{
		User:         user,
		Achievements: achs,
		BestStreak:   domain.BestStreak(streaks, s.now()),
	})
}
