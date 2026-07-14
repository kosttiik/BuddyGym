package httpapi

import (
	"net/http"
	"slices"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

// free themes shipped with the MVP; paid ones come later
var allowedThemes = []string{"default", "dark", "neon"}

type MeResponse struct {
	User domain.User `json:"user"`
	// the whole catalog, earned or not: a locked one carries its progress
	Achievements []domain.AchievementProgress `json:"achievements"`
	Stats        domain.Stats                 `json:"stats"`
	// highest streak across the user rooms
	BestStreak int `json:"best_streak"`
}

// Every field is optional: a nil one is left alone, an empty string clears it.
type UpdateMeRequest struct {
	Theme       *string `json:"theme,omitempty" example:"dark" enums:"default,dark,neon"`
	StatusEmoji *string `json:"status_emoji,omitempty"`
	StatusText  *string `json:"status_text,omitempty" example:"На массе"`
}

// handleGetMe godoc
//
//	@Summary		Get my profile
//	@Description	Returns the authenticated user profile with achievements.
//	@Tags			me
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	MeResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/me [get]
func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	progress, stats, err := s.profile(r, user.ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, MeResponse{
		User:         user,
		Achievements: progress,
		Stats:        stats,
		BestStreak:   stats.BestStreak,
	})
}

// profile gathers the achievement catalog folded against the user stats.
func (s *Server) profile(r *http.Request, userID int64) ([]domain.AchievementProgress, domain.Stats, error) {
	stats, err := s.users.Stats(r.Context(), userID)
	if err != nil {
		return nil, domain.Stats{}, err
	}
	streaks, err := s.streaks.StreaksByUser(r.Context(), userID)
	if err != nil {
		return nil, domain.Stats{}, err
	}
	stats.BestStreak = domain.BestStreak(streaks, s.now())

	granted, err := s.users.Achievements(r.Context(), userID)
	if err != nil {
		return nil, domain.Stats{}, err
	}
	return domain.Progress(stats, granted), stats, nil
}

// handlePatchMe godoc
//
//	@Summary		Update my profile
//	@Description	Changes the theme and the status line. Every field is optional; only the ones present are written. Empty strings clear the status. The rank is derived from workouts and cannot be set.
//	@Tags			me
//	@Security		BearerAuth
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
	userID := userFrom(r.Context()).ID
	user := userFrom(r.Context())

	if req.Theme != nil {
		if !slices.Contains(allowedThemes, *req.Theme) {
			writeErr(w, http.StatusBadRequest, "unknown theme")
			return
		}
		updated, err := s.users.UpdateTheme(r.Context(), userID, *req.Theme)
		if err != nil {
			s.mapError(w, err)
			return
		}
		user = updated
	}

	// the status is a pair: sending one half alone would leave a stray emoji or a naked line
	if req.StatusEmoji != nil || req.StatusText != nil {
		emoji, text := user.StatusEmoji, user.StatusText
		if req.StatusEmoji != nil {
			emoji = *req.StatusEmoji
		}
		if req.StatusText != nil {
			text = *req.StatusText
		}
		emoji, err := domain.NormalizeStatusEmoji(emoji)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		text, err = domain.NormalizeStatusText(text)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		updated, err := s.users.SetStatus(r.Context(), userID, emoji, text)
		if err != nil {
			s.mapError(w, err)
			return
		}
		user = updated
	}

	writeJSON(w, http.StatusOK, user)
}
