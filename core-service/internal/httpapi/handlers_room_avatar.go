package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

const maxRoomAvatar = 5 << 20

func roomAvatarID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("avatarId"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) streamAvatar(w http.ResponseWriter, r *http.Request, key string, roomID int64) {
	body, contentType, err := s.avatars.Open(r.Context(), key)
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
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if _, err := io.Copy(w, body); err != nil {
		s.log.Error("streaming room avatar failed", "room_id", roomID, "err", err)
	}
}

// @Summary		Download the current room picture
// @Description	Streams the newest picture of the room from private object storage. Requires a Bearer token, so browsers must fetch it via XHR rather than a plain <img src>. Returns 404 when the room has no picture.
// @Tags			rooms
// @Security		BearerAuth
// @Produce		image/jpeg
// @Param			id	path		int	true	"room id"
// @Success		200	{file}		binary
// @Failure		400	{object}	ErrorResponse
// @Failure		401	{object}	ErrorResponse
// @Failure		404	{object}	ErrorResponse
// @Router			/rooms/{id}/avatar [get]
func (s *Server) handleGetRoomAvatar(w http.ResponseWriter, r *http.Request) {
	id, ok := roomID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid room id")
		return
	}
	room, err := s.rooms.Get(r.Context(), id)
	if err != nil {
		s.mapError(w, err)
		return
	}
	if s.avatars == nil || room.AvatarKey == "" {
		writeErr(w, http.StatusNotFound, "room has no picture")
		return
	}
	s.streamAvatar(w, r, room.AvatarKey, room.ID)
}

// @Summary		List the room picture gallery
// @Description	Room members only. Newest first; the first entry is the picture the room currently wears.
// @Tags			rooms
// @Security		BearerAuth
// @Produce		json
// @Param			id	path		int	true	"room id"
// @Success		200	{array}		domain.RoomAvatar
// @Failure		400	{object}	ErrorResponse
// @Failure		401	{object}	ErrorResponse
// @Failure		403	{object}	ErrorResponse
// @Failure		404	{object}	ErrorResponse
// @Failure		500	{object}	ErrorResponse
// @Router			/rooms/{id}/avatars [get]
func (s *Server) handleListRoomAvatars(w http.ResponseWriter, r *http.Request) {
	room, ok := s.membership(w, r)
	if !ok {
		return
	}
	avatars, err := s.rooms.ListAvatars(r.Context(), room.ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	if avatars == nil {
		avatars = []domain.RoomAvatar{}
	}
	writeJSON(w, http.StatusOK, avatars)
}

// @Summary		Download one picture from the gallery
// @Description	Room members only. Serves an older picture the room used to wear.
// @Tags			rooms
// @Security		BearerAuth
// @Produce		image/jpeg
// @Param			id			path		int	true	"room id"
// @Param			avatarId	path		int	true	"picture id"
// @Success		200			{file}		binary
// @Failure		400			{object}	ErrorResponse
// @Failure		401			{object}	ErrorResponse
// @Failure		403			{object}	ErrorResponse
// @Failure		404			{object}	ErrorResponse
// @Router			/rooms/{id}/avatars/{avatarId} [get]
func (s *Server) handleGetRoomAvatarByID(w http.ResponseWriter, r *http.Request) {
	room, ok := s.membership(w, r)
	if !ok {
		return
	}
	avatarID, ok := roomAvatarID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid picture id")
		return
	}
	avatar, err := s.rooms.GetAvatar(r.Context(), room.ID, avatarID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	if s.avatars == nil {
		writeErr(w, http.StatusNotFound, "room has no picture")
		return
	}
	s.streamAvatar(w, r, avatar.ObjectKey, room.ID)
}

// @Summary		Add a room picture
// @Description	Any room member may add one. Send multipart/form-data with a "photo" file up to 5 MB. The new picture becomes the face of the room; the previous ones stay in the gallery.
// @Tags			rooms
// @Security		BearerAuth
// @Accept			multipart/form-data
// @Produce		json
// @Param			id		path		int		true	"room id"
// @Param			photo	formData	file	true	"room picture"
// @Success		201		{object}	domain.RoomAvatar
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Failure		403		{object}	ErrorResponse
// @Failure		404		{object}	ErrorResponse
// @Failure		500		{object}	ErrorResponse
// @Router			/rooms/{id}/avatar [put]
func (s *Server) handleAddRoomAvatar(w http.ResponseWriter, r *http.Request) {
	room, ok := s.membership(w, r)
	if !ok {
		return
	}
	if s.avatars == nil {
		writeErr(w, http.StatusServiceUnavailable, "picture storage is unavailable")
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
	if len(photo) == 0 || len(photo) > maxRoomAvatar {
		writeErr(w, http.StatusBadRequest, "photo must be 1 byte .. 5 MB")
		return
	}
	if !isImage(photo) {
		writeErr(w, http.StatusBadRequest, "photo must be JPEG, PNG, GIF, WebP or HEIC")
		return
	}

	key := domain.RoomAvatarKey(room.ID)
	if err := s.avatars.Put(r.Context(), key, photo); err != nil {
		s.internal(w, err)
		return
	}
	added, err := s.rooms.AddAvatar(r.Context(), room.ID, userFrom(r.Context()).ID, key)
	if err != nil {
		if delErr := s.avatars.Delete(r.Context(), key); delErr != nil {
			s.log.Error("dropping orphan room avatar failed", "room_id", room.ID, "err", delErr)
		}
		s.mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, added)
}

// @Summary		Delete a room picture
// @Description	The member who uploaded it or the room creator. When the current picture goes, the room falls back to the newest one left.
// @Tags			rooms
// @Security		BearerAuth
// @Param			id			path	int	true	"room id"
// @Param			avatarId	path	int	true	"picture id"
// @Success		204
// @Failure		400	{object}	ErrorResponse
// @Failure		401	{object}	ErrorResponse
// @Failure		403	{object}	ErrorResponse
// @Failure		404	{object}	ErrorResponse
// @Failure		500	{object}	ErrorResponse
// @Router			/rooms/{id}/avatars/{avatarId} [delete]
func (s *Server) handleDeleteRoomAvatar(w http.ResponseWriter, r *http.Request) {
	room, ok := s.membership(w, r)
	if !ok {
		return
	}
	avatarID, ok := roomAvatarID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid picture id")
		return
	}
	avatar, err := s.rooms.GetAvatar(r.Context(), room.ID, avatarID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	user := userFrom(r.Context()).ID
	if avatar.UploadedBy != user && room.CreatorID != user {
		writeErr(w, http.StatusForbidden, "uploader or room creator only")
		return
	}

	key, err := s.rooms.DeleteAvatar(r.Context(), room.ID, avatarID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	if s.avatars != nil {
		if err := s.avatars.Delete(r.Context(), key); err != nil {
			s.log.Error("deleting room avatar object failed", "room_id", room.ID, "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
