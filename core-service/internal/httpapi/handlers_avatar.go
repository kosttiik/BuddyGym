package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
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

// @Summary		Upload my avatar
// @Tags			me
// @Security		BearerAuth
// @Accept			multipart/form-data
// @Produce		json
// @Param			photo	formData	file	true	"profile picture"
// @Success		200		{object}	domain.User
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Failure		503		{object}	ErrorResponse
// @Router			/me/avatar [put]
func (s *Server) handlePutMyAvatar(w http.ResponseWriter, r *http.Request) {
	if s.avatars == nil || s.accounts == nil {
		writeErr(w, http.StatusServiceUnavailable, "avatar storage is unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRoomAvatar+1<<20)
	if err := r.ParseMultipartForm(maxRoomAvatar); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("photo")
	if err != nil {
		writeErr(w, http.StatusBadRequest, `multipart "photo" file is required`)
		return
	}
	defer file.Close()
	photo, err := io.ReadAll(io.LimitReader(file, maxRoomAvatar+1))
	if err != nil {
		s.internal(w, err)
		return
	}
	if len(photo) == 0 || len(photo) > maxRoomAvatar || !isImage(photo) {
		writeErr(w, http.StatusBadRequest, "photo must be JPEG, PNG or WebP up to 5 MB")
		return
	}
	userID := userFrom(r.Context()).ID
	key := domain.UserAvatarKey(userID)
	if err := s.avatars.Put(r.Context(), key, photo); err != nil {
		s.internal(w, err)
		return
	}
	user, oldKey, err := s.accounts.SetCustomAvatar(r.Context(), userID, key)
	if err != nil {
		_ = s.avatars.Delete(r.Context(), key)
		s.mapError(w, err)
		return
	}
	if oldKey != "" && oldKey != key {
		if err := s.avatars.Delete(r.Context(), oldKey); err != nil {
			s.log.Error("deleting replaced avatar failed", "user_id", userID, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, user)
}

// @Summary		Delete my custom avatar
// @Tags			me
// @Security		BearerAuth
// @Produce		json
// @Success		200	{object}	domain.User
// @Failure		401	{object}	ErrorResponse
// @Failure		503	{object}	ErrorResponse
// @Router			/me/avatar [delete]
func (s *Server) handleDeleteMyAvatar(w http.ResponseWriter, r *http.Request) {
	if s.avatars == nil || s.accounts == nil {
		writeErr(w, http.StatusServiceUnavailable, "avatar storage is unavailable")
		return
	}
	userID := userFrom(r.Context()).ID
	user, oldKey, err := s.accounts.ClearCustomAvatar(r.Context(), userID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	if oldKey != "" {
		if err := s.avatars.Delete(r.Context(), oldKey); err != nil {
			s.log.Error("deleting custom avatar failed", "user_id", userID, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, user)
}
