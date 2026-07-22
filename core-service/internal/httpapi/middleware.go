package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kosttiik/BuddyGym/core-service/internal/auth"
	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
	"github.com/kosttiik/BuddyGym/core-service/internal/storage"
)

type ctxKey int

const userKey ctxKey = iota

func userFrom(ctx context.Context) domain.User {
	u, _ := ctx.Value(userKey).(domain.User)
	return u
}

const bearerScheme = "Bearer "

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(header, bearerScheme)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "expected Authorization: Bearer <token>")
			return
		}
		userID, err := auth.VerifyToken(s.jwtSecret, token, s.now())
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		if !s.allow(w, r, s.apiLimiter, strconv.FormatInt(userID, 10)) {
			return
		}
		user, err := s.users.Get(r.Context(), userID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeErr(w, http.StatusUnauthorized, "unknown user")
				return
			}
			s.internal(w, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	}
}

func (s *Server) allow(w http.ResponseWriter, r *http.Request, limiter RateLimiter, key string) bool {
	if limiter == nil {
		return true
	}
	ok, err := limiter.Allow(r.Context(), key)
	if err != nil {
		s.internal(w, err)
		return false
	}
	if !ok {
		writeErr(w, http.StatusTooManyRequests, "too many requests")
		return false
	}
	return true
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r)
		s.log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"dur", time.Since(start).Round(time.Millisecond).String(),
		)
	})
}
