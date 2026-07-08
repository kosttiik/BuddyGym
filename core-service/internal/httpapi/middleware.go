package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/kosttiik/BuddyGym/core-service/internal/auth"
	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type ctxKey int

const userKey ctxKey = iota

func userFrom(ctx context.Context) domain.User {
	u, _ := ctx.Value(userKey).(domain.User)
	return u
}

const authScheme = "tma "

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, authScheme) {
			writeErr(w, http.StatusUnauthorized, "expected Authorization: tma <initData>")
			return
		}
		tg, err := auth.Validate(header[len(authScheme):], s.botToken, s.authTTL, s.now())
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid init data")
			return
		}
		user, err := s.users.Upsert(r.Context(), tg.ID, tg.Username, tg.FirstName, tg.PhotoURL)
		if err != nil {
			s.internal(w, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	}
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
