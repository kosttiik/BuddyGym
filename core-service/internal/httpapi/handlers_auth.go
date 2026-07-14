package httpapi

import (
	"net"
	"net/http"

	"github.com/kosttiik/BuddyGym/core-service/internal/auth"
	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type AuthTelegramRequest struct {
	InitData string `json:"init_data" example:"query_id=...&user=...&auth_date=...&hash=..."`
}

type AuthTelegramResponse struct {
	Token string      `json:"token"`
	User  domain.User `json:"user"`
}

// handleAuthTelegram godoc
//
//	@Summary		Exchange Telegram initData for a JWT
//	@Description	Validates Mini App initData signature, upserts the user and issues a Bearer token for all other endpoints.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		AuthTelegramRequest	true	"raw initData string"
//	@Success		200		{object}	AuthTelegramResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		429		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/auth/telegram [post]
func (s *Server) handleAuthTelegram(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r, s.authLimiter, clientIP(r)) {
		return
	}
	var req AuthTelegramRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.InitData == "" {
		writeErr(w, http.StatusBadRequest, "init_data is required")
		return
	}
	tg, err := auth.Validate(req.InitData, s.botToken, s.authTTL, s.now())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid init data")
		return
	}
	user, err := s.users.Upsert(r.Context(), tg.ID, tg.Username, tg.FirstName, tg.PhotoURL)
	if err != nil {
		s.internal(w, err)
		return
	}
	if s.avatarMirror != nil {
		s.avatarMirror.SyncInBackground(user.ID, tg.PhotoURL, user.AvatarSource)
	}
	token, err := auth.IssueToken(s.jwtSecret, user.ID, s.jwtTTL, s.now())
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, AuthTelegramResponse{Token: token, User: user})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
