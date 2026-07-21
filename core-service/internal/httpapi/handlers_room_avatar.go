package httpapi

import (
	"io"
	"net/http"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

const maxRoomAvatar = 5 << 20

// roomAdmin loads the room and refuses everyone but its creator.
func (s *Server) roomAdmin(w http.ResponseWriter, r *http.Request) (domain.Room, bool) {
	id, ok := roomID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid room id")
		return domain.Room{}, false
	}
	room, err := s.rooms.Get(r.Context(), id)
	if err != nil {
		s.mapError(w, err)
		return domain.Room{}, false
	}
	if room.CreatorID != userFrom(r.Context()).ID {
		writeErr(w, http.StatusForbidden, "room creator only")
		return domain.Room{}, false
	}
	return room, true
}

// handleGetRoomAvatar godoc
//
//	@Summary		Download a room picture
//	@Description	Streams the room picture from private object storage. Requires a Bearer token, so browsers must fetch it via XHR rather than a plain <img src>. Returns 404 when the room has no picture.
//	@Tags			rooms
//	@Security		BearerAuth
//	@Produce		image/jpeg
//	@Param			id	path		int	true	"room id"
//	@Success		200	{file}		binary
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Router			/rooms/{id}/avatar [get]
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

	body, contentType, err := s.avatars.Open(r.Context(), room.AvatarKey)
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
	// the key is derived from the room id, so a replaced picture reuses it
	w.Header().Set("Cache-Control", "private, max-age=60")
	if _, err := io.Copy(w, body); err != nil {
		s.log.Error("streaming room avatar failed", "room_id", id, "err", err)
	}
}

// handleSetRoomAvatar godoc
//
//	@Summary		Upload a room picture
//	@Description	Room creator only. Send multipart/form-data with a "photo" file up to 5 MB. Replaces the previous picture.
//	@Tags			rooms
//	@Security		BearerAuth
//	@Accept			multipart/form-data
//	@Produce		json
//	@Param			id		path		int		true	"room id"
//	@Param			photo	formData	file	true	"room picture"
//	@Success		200		{object}	domain.Room
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/rooms/{id}/avatar [put]
func (s *Server) handleSetRoomAvatar(w http.ResponseWriter, r *http.Request) {
	room, ok := s.roomAdmin(w, r)
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
	if err := s.rooms.SetAvatar(r.Context(), room.ID, key); err != nil {
		s.mapError(w, err)
		return
	}
	room.AvatarKey = key
	room.HasAvatar = true
	writeJSON(w, http.StatusOK, room)
}

// handleDeleteRoomAvatar godoc
//
//	@Summary		Remove a room picture
//	@Description	Room creator only. The room keeps its letter placeholder afterwards.
//	@Tags			rooms
//	@Security		BearerAuth
//	@Param			id	path	int	true	"room id"
//	@Success		204
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/rooms/{id}/avatar [delete]
func (s *Server) handleDeleteRoomAvatar(w http.ResponseWriter, r *http.Request) {
	room, ok := s.roomAdmin(w, r)
	if !ok {
		return
	}
	if room.AvatarKey == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.rooms.SetAvatar(r.Context(), room.ID, ""); err != nil {
		s.mapError(w, err)
		return
	}
	// the row is what makes the picture reachable, so a failed object delete only leaks bytes
	if s.avatars != nil {
		if err := s.avatars.Delete(r.Context(), room.AvatarKey); err != nil {
			s.log.Error("deleting room avatar object failed", "room_id", room.ID, "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
