package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kosttiik/BuddyGym/core-service/internal/storage"
)

type ErrorResponse struct {
	Error string `json:"error" example:"room not found"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, ErrorResponse{Error: msg})
}

func (s *Server) internal(w http.ResponseWriter, err error) {
	s.log.Error("internal error", "err", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

// mapError translates storage and checkin-service gRPC errors to HTTP.
func (s *Server) mapError(w http.ResponseWriter, err error) {
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.NotFound:
			writeErr(w, http.StatusNotFound, st.Message())
			return
		case codes.InvalidArgument:
			writeErr(w, http.StatusBadRequest, st.Message())
			return
		case codes.AlreadyExists, codes.FailedPrecondition:
			writeErr(w, http.StatusConflict, st.Message())
			return
		case codes.PermissionDenied:
			writeErr(w, http.StatusForbidden, st.Message())
			return
		case codes.Unavailable, codes.DeadlineExceeded:
			s.log.Error("checkin-service unavailable", "err", err)
			writeErr(w, http.StatusBadGateway, "checkin service unavailable")
			return
		}
	}
	s.internal(w, err)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return false
	}
	return true
}
