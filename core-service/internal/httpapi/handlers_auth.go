package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kosttiik/BuddyGym/core-service/internal/auth"
	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
	"github.com/kosttiik/BuddyGym/core-service/internal/storage"
)

type AuthTelegramRequest struct {
	InitData  string `json:"init_data" example:"query_id=...&user=...&auth_date=...&hash=..."`
	LinkToken string `json:"link_token,omitempty"`
}

type AuthTelegramResponse struct {
	Token string      `json:"token"`
	User  domain.User `json:"user"`
}

type AuthPasswordRequest struct {
	Username  string `json:"username" example:"kostik"`
	Password  string `json:"password" example:"correct horse battery staple" minLength:"8" maxLength:"128"`
	FirstName string `json:"first_name,omitempty" example:"Константин"`
}

type SetCredentialsRequest struct {
	Username        string `json:"username" example:"kostik"`
	Password        string `json:"password" example:"correct horse battery staple" minLength:"8" maxLength:"128"`
	CurrentPassword string `json:"current_password,omitempty"`
}

type TelegramLinkResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

const (
	refreshCookie     = "bg.refresh"
	minPasswordLength = 8
	maxPasswordLength = 128
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9_]{3,32}$`)

func normalizeUsername(raw string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, "@")))
}

func validPassword(password string) bool {
	n := utf8.RuneCountInString(password)
	return n >= minPasswordLength && n <= maxPasswordLength
}

func validName(name string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(name))
	return n >= 1 && n <= 64
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func conflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Server) issueAuth(
	w http.ResponseWriter,
	r *http.Request,
	user domain.User,
	authMethod string,
) {
	w.Header().Set("Cache-Control", "no-store")
	token, err := auth.IssueTokenWithMethod(
		s.jwtSecret,
		user.ID,
		authMethod,
		s.jwtTTL,
		s.now(),
	)
	if err != nil {
		s.internal(w, err)
		return
	}
	if s.accounts != nil && s.refreshTTL > 0 && authMethod != auth.AuthMethodTelegram {
		refresh, err := randomToken()
		if err != nil {
			s.internal(w, err)
			return
		}
		expires := s.now().Add(s.refreshTTL)
		if err := s.accounts.CreateSession(r.Context(), user.ID, refresh, expires); err != nil {
			s.internal(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     refreshCookie,
			Value:    refresh,
			Path:     "/api/v1/auth",
			Expires:  expires,
			MaxAge:   int(s.refreshTTL.Seconds()),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})
	}
	writeJSON(w, http.StatusOK, AuthTelegramResponse{Token: token, User: user})
}

// @Summary		Exchange Telegram initData for a JWT
// @Description	Validates Mini App initData signature, upserts the user and issues a Bearer token for all other endpoints.
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body		AuthTelegramRequest	true	"raw initData string"
// @Success		200		{object}	AuthTelegramResponse
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Failure		429		{object}	ErrorResponse
// @Failure		500		{object}	ErrorResponse
// @Router			/auth/telegram [post]
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
	var user domain.User
	if s.accounts == nil {
		user, err = s.users.Upsert(r.Context(), tg.ID, tg.Username, tg.FirstName, tg.PhotoURL)
	} else if req.LinkToken != "" {
		user, err = s.accounts.LinkTelegram(r.Context(), req.LinkToken, tg.ID)
	} else {
		user, err = s.accounts.UserByTelegram(r.Context(), tg.ID)
		if errors.Is(err, storage.ErrNotFound) {
			username := normalizeUsername(tg.Username)
			user, err = s.accounts.CreateTelegramUser(
				r.Context(),
				tg.ID,
				username,
				tg.FirstName,
				tg.PhotoURL,
			)
			if conflict(err) {
				user, err = s.accounts.UserByTelegram(r.Context(), tg.ID)
				if errors.Is(err, storage.ErrNotFound) && username != "" {
					user, err = s.accounts.CreateTelegramUser(
						r.Context(),
						tg.ID,
						"",
						tg.FirstName,
						tg.PhotoURL,
					)
				}
			}
		}
	}
	if err != nil {
		if conflict(err) {
			writeErr(w, http.StatusConflict, "telegram account is already linked")
		} else if errors.Is(err, storage.ErrNotFound) {
			writeErr(w, http.StatusUnauthorized, "invalid or expired link")
		} else {
			s.internal(w, err)
		}
		return
	}
	if s.avatarMirror != nil {
		s.avatarMirror.SyncInBackground(user.ID, tg.ID, tg.PhotoURL, user.AvatarSource)
	}
	s.issueAuth(w, r, user, auth.AuthMethodTelegram)
}

// @Summary		Register with a username and password
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body		AuthPasswordRequest	true	"new account"
// @Success		200		{object}	AuthTelegramResponse
// @Failure		400		{object}	ErrorResponse
// @Failure		409		{object}	ErrorResponse
// @Failure		429		{object}	ErrorResponse
// @Failure		500		{object}	ErrorResponse
// @Router			/auth/register [post]
func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r, s.authLimiter, clientIP(r)) {
		return
	}
	if s.accounts == nil {
		writeErr(w, http.StatusServiceUnavailable, "web registration is unavailable")
		return
	}
	var req AuthPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Username = normalizeUsername(req.Username)
	req.FirstName = strings.TrimSpace(req.FirstName)
	if !usernamePattern.MatchString(req.Username) {
		writeErr(w, http.StatusBadRequest, "username must be 3..32 lowercase letters, digits or underscores")
		return
	}
	if !validName(req.FirstName) {
		writeErr(w, http.StatusBadRequest, "first_name must be 1..64 characters")
		return
	}
	if !validPassword(req.Password) {
		writeErr(w, http.StatusBadRequest, "password must be 8..128 characters")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.internal(w, err)
		return
	}
	user, err := s.accounts.CreateLocalUser(r.Context(), req.Username, req.FirstName, hash)
	if err != nil {
		if conflict(err) {
			writeErr(w, http.StatusConflict, "username is unavailable")
		} else {
			s.internal(w, err)
		}
		return
	}
	s.issueAuth(w, r, user, auth.AuthMethodPassword)
}

// @Summary		Log in with a username and password
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body		AuthPasswordRequest	true	"credentials"
// @Success		200		{object}	AuthTelegramResponse
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Failure		429		{object}	ErrorResponse
// @Router			/auth/login [post]
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r, s.authLimiter, clientIP(r)) {
		return
	}
	if s.accounts == nil {
		writeErr(w, http.StatusServiceUnavailable, "web login is unavailable")
		return
	}
	var req AuthPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	user, hash, err := s.accounts.PasswordUser(r.Context(), normalizeUsername(req.Username))
	if err != nil || !auth.VerifyPassword(hash, req.Password) {
		writeErr(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	s.issueAuth(w, r, user, auth.AuthMethodPassword)
}

// @Summary		Rotate a refresh session
// @Tags			auth
// @Produce		json
// @Success		200	{object}	AuthTelegramResponse
// @Failure		401	{object}	ErrorResponse
// @Router			/auth/refresh [post]
func (s *Server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.accounts == nil {
		writeErr(w, http.StatusUnauthorized, "invalid session")
		return
	}
	cookie, err := r.Cookie(refreshCookie)
	if err != nil || cookie.Value == "" {
		writeErr(w, http.StatusUnauthorized, "invalid session")
		return
	}
	next, err := randomToken()
	if err != nil {
		s.internal(w, err)
		return
	}
	expires := s.now().Add(s.refreshTTL)
	user, err := s.accounts.RotateSession(r.Context(), cookie.Value, next, expires)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookie,
		Value:    next,
		Path:     "/api/v1/auth",
		Expires:  expires,
		MaxAge:   int(s.refreshTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	token, err := auth.IssueTokenWithMethod(
		s.jwtSecret,
		user.ID,
		auth.AuthMethodSession,
		s.jwtTTL,
		s.now(),
	)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, AuthTelegramResponse{Token: token, User: user})
}

// @Summary		Log out the current refresh session
// @Tags			auth
// @Success		204
// @Router			/auth/logout [post]
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(refreshCookie); err == nil && s.accounts != nil {
		if err := s.accounts.RevokeSession(r.Context(), cookie.Value); err != nil {
			s.internal(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookie,
		Value:    "",
		Path:     "/api/v1/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Set or replace web credentials
// @Tags			me
// @Security		BearerAuth
// @Accept			json
// @Produce		json
// @Param			body	body		SetCredentialsRequest	true	"web credentials"
// @Success		200		{object}	AuthTelegramResponse
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Failure		409		{object}	ErrorResponse
// @Router			/me/credentials [patch]
func (s *Server) handleSetCredentials(w http.ResponseWriter, r *http.Request) {
	if s.accounts == nil {
		writeErr(w, http.StatusServiceUnavailable, "web login is unavailable")
		return
	}
	var req SetCredentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Username = normalizeUsername(req.Username)
	if !usernamePattern.MatchString(req.Username) {
		writeErr(w, http.StatusBadRequest, "username must be 3..32 lowercase letters, digits or underscores")
		return
	}
	if !validPassword(req.Password) {
		writeErr(w, http.StatusBadRequest, "password must be 8..128 characters")
		return
	}
	userID := userFrom(r.Context()).ID
	state, err := s.accounts.State(r.Context(), userID)
	if err != nil {
		s.internal(w, err)
		return
	}
	if state.WebLoginEnabled && authMethodFrom(r.Context()) != auth.AuthMethodTelegram {
		currentHash, err := s.accounts.CredentialHash(r.Context(), userID)
		if err != nil || !auth.VerifyPassword(currentHash, req.CurrentPassword) {
			writeErr(w, http.StatusUnauthorized, "current password is invalid")
			return
		}
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.internal(w, err)
		return
	}
	user, err := s.accounts.SetCredentials(r.Context(), userID, req.Username, hash)
	if err != nil {
		if conflict(err) {
			writeErr(w, http.StatusConflict, "username is unavailable")
		} else {
			s.internal(w, err)
		}
		return
	}
	s.issueAuth(w, r, user, auth.AuthMethodPassword)
}

// @Summary		Create a Telegram account-link token
// @Tags			me
// @Security		BearerAuth
// @Produce		json
// @Success		201	{object}	TelegramLinkResponse
// @Failure		401	{object}	ErrorResponse
// @Router			/me/telegram-link [post]
func (s *Server) handleCreateTelegramLink(w http.ResponseWriter, r *http.Request) {
	if s.accounts == nil {
		writeErr(w, http.StatusServiceUnavailable, "account linking is unavailable")
		return
	}
	token, err := randomToken()
	if err != nil {
		s.internal(w, err)
		return
	}
	expires := s.now().Add(10 * time.Minute)
	if err := s.accounts.CreateLinkToken(r.Context(), userFrom(r.Context()).ID, token, expires); err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, TelegramLinkResponse{
		Token:     token,
		ExpiresAt: expires.UTC().Format(time.RFC3339),
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
