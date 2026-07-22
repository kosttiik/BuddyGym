package httpapi

import (
	"io"
	"net/http"
	"strconv"
)

var allowedAvatarTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

// @Summary		Download a user avatar
// @Description	Streams the mirrored Telegram profile picture. Telegram serves avatars from hosts our users cannot reach, so core keeps a copy in private object storage and proxies the bytes. Requires a Bearer token, so browsers must fetch it via XHR rather than a plain <img src>. Returns 404 when the user has no avatar.
// @Tags			users
// @Security		BearerAuth
// @Produce		image/jpeg
// @Param			id	path		int	true	"user id"
// @Success		200	{file}		binary
// @Failure		400	{object}	ErrorResponse
// @Failure		401	{object}	ErrorResponse
// @Failure		404	{object}	ErrorResponse
// @Router			/users/{id}/avatar [get]
func (s *Server) handleGetAvatar(w http.ResponseWriter, r *http.Request) {
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
	if s.avatars == nil || user.AvatarKey == "" {
		writeErr(w, http.StatusNotFound, "user has no avatar")
		return
	}

	body, contentType, err := s.avatars.Open(r.Context(), user.AvatarKey)
	if err != nil {
		s.mapError(w, err)
		return
	}
	defer body.Close()

	if !allowedAvatarTypes[contentType] {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=60")
	if _, err := io.Copy(w, body); err != nil {
		s.log.Error("streaming avatar failed", "user_id", id, "err", err)
	}
}
