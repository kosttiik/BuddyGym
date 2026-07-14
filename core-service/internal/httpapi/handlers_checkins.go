package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	pbv1 "github.com/kosttiik/BuddyGym/core-service/internal/pb/buddygym/v1"

	"github.com/kosttiik/BuddyGym/core-service/internal/checkin"
)

const (
	maxPhotoSize    = 10 << 20
	maxCheckinRooms = 20
)

var allowedPhotoTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
	"image/heic": true,
}

// isImage sniffs magic bytes. A client-declared content type proves nothing, and an
// SVG or HTML payload stored as a "photo" would be served back as active content.
func isImage(b []byte) bool {
	switch {
	case bytes.HasPrefix(b, []byte("\xff\xd8\xff")):
		return true
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return true
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return true
	case len(b) >= 12 && bytes.Equal(b[0:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return true
	case len(b) >= 12 && bytes.Equal(b[4:8], []byte("ftyp")):
		return true
	}
	return false
}

type CreateCheckinGeoRequest struct {
	RoomIDs  []int64     `json:"room_ids"`
	BuddyIDs []int64     `json:"buddy_ids"`
	Geo      checkin.Geo `json:"geo"`
}

type VoteRequest struct {
	Approve bool `json:"approve" example:"true"`
}

// handleCreateCheckin godoc
//
//	@Summary		Create a checkin in one or more rooms
//	@Description	Submits one workout proof to every listed room. A photo is uploaded once and shared by all of them, so posting to several rooms never stores it twice. Send multipart/form-data with a "photo" file (up to 10 MB) and repeated "room_ids" fields, or JSON with a geo point and room_ids for the fast path. The caller must be a member of every room.
//	@Tags			checkins
//	@Security		BearerAuth
//	@Accept			mpfd
//	@Accept			json
//	@Produce		json
//	@Param			photo		formData	file					false	"workout photo"
//	@Param			room_ids	formData	[]int					false	"rooms to submit to"
//	@Param			buddy_ids	formData	[]int					false	"members who trained with you"
//	@Param			body		body		CreateCheckinGeoRequest	false	"geo proof (json variant)"
//	@Success		201			{array}		checkin.Checkin
//	@Failure		400			{object}	ErrorResponse
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse
//	@Failure		404			{object}	ErrorResponse
//	@Failure		429			{object}	ErrorResponse
//	@Failure		502			{object}	ErrorResponse
//	@Router			/checkins [post]
func (s *Server) handleCreateCheckin(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	if !s.allow(w, r, s.checkinLimiter, strconv.FormatInt(user.ID, 10)) {
		return
	}

	var photo []byte
	var geo *checkin.Geo
	var roomIDs []int64
	var buddyIDs []int64

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxPhotoSize+1<<20)
		file, _, err := r.FormFile("photo")
		if err != nil {
			writeErr(w, http.StatusBadRequest, `multipart "photo" file is required`)
			return
		}
		defer file.Close()
		photo, err = io.ReadAll(io.LimitReader(file, maxPhotoSize+1))
		if err != nil {
			s.internal(w, err)
			return
		}
		if len(photo) == 0 || len(photo) > maxPhotoSize {
			writeErr(w, http.StatusBadRequest, "photo must be 1 byte .. 10 MB")
			return
		}
		if !isImage(photo) {
			writeErr(w, http.StatusBadRequest, "photo must be JPEG, PNG, GIF, WebP or HEIC")
			return
		}
		roomIDs, err = parseIDs(r.MultipartForm.Value["room_ids"], "room_ids")
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		buddyIDs, err = parseIDs(r.MultipartForm.Value["buddy_ids"], "buddy_ids")
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		var req CreateCheckinGeoRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Geo.Lat < -90 || req.Geo.Lat > 90 || req.Geo.Lon < -180 || req.Geo.Lon > 180 ||
			(req.Geo.Lat == 0 && req.Geo.Lon == 0) {
			writeErr(w, http.StatusBadRequest, "invalid geo point")
			return
		}
		geo = &req.Geo
		roomIDs = req.RoomIDs
		buddyIDs = req.BuddyIDs
	}

	targets, ok := s.resolveTargets(w, r, roomIDs)
	if !ok {
		return
	}

	created, err := s.checkins.Create(r.Context(), user.ID, targets, photo, geo)
	if err != nil {
		s.mapError(w, err)
		return
	}

	for _, c := range created {
		buddies, ok := s.resolveBuddies(w, r, c.RoomID, buddyIDs)
		if !ok {
			return
		}
		if err := s.buddies.Tag(r.Context(), c.ID, c.RoomID, user.ID, buddies); err != nil {
			s.internal(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, s.enrichBuddies(r, created))
}

// resolveTargets rejects the whole request unless the caller is a member of every
// room, so a checkin can never be planted in a room the user does not belong to.
func (s *Server) resolveTargets(w http.ResponseWriter, r *http.Request, roomIDs []int64) ([]checkin.Target, bool) {
	if len(roomIDs) == 0 || len(roomIDs) > maxCheckinRooms {
		writeErr(w, http.StatusBadRequest, "room_ids must list 1.."+strconv.Itoa(maxCheckinRooms)+" rooms")
		return nil, false
	}

	user := userFrom(r.Context())
	seen := make(map[int64]struct{}, len(roomIDs))
	targets := make([]checkin.Target, 0, len(roomIDs))

	for _, roomID := range roomIDs {
		if _, dup := seen[roomID]; dup {
			writeErr(w, http.StatusBadRequest, "room_ids must not repeat a room")
			return nil, false
		}
		seen[roomID] = struct{}{}

		room, err := s.rooms.Get(r.Context(), roomID)
		if err != nil {
			s.mapError(w, err)
			return nil, false
		}
		member, err := s.rooms.IsMember(r.Context(), roomID, user.ID)
		if err != nil {
			s.internal(w, err)
			return nil, false
		}
		if !member {
			writeErr(w, http.StatusForbidden, "room members only")
			return nil, false
		}
		targets = append(targets, checkin.Target{
			RoomID:        room.ID,
			VotesRequired: int32(room.VotesRequired),
		})
	}
	return targets, true
}

func parseIDs(values []string, field string) ([]int64, error) {
	ids := make([]int64, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || id <= 0 {
				return nil, errors.New(field + " must be positive integers")
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// handleGetCheckinPhoto godoc
//
//	@Summary		Download a checkin photo
//	@Description	Streams the proof photo. The object storage bucket is private: only members of the room the checkin belongs to can read it, and the bytes are proxied through core-service. Requires a Bearer token, so browsers must fetch it via XHR rather than a plain <img src>.
//	@Tags			checkins
//	@Security		BearerAuth
//	@Produce		image/jpeg
//	@Produce		image/png
//	@Param			id	path		string	true	"checkin id"
//	@Success		200	{file}		binary
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		502	{object}	ErrorResponse
//	@Router			/checkins/{id}/photo [get]
func (s *Server) handleGetCheckinPhoto(w http.ResponseWriter, r *http.Request) {
	checkinID := r.PathValue("id")
	if checkinID == "" {
		writeErr(w, http.StatusBadRequest, "invalid checkin id")
		return
	}

	target, err := s.checkins.Get(r.Context(), checkinID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	if !target.HasPhoto {
		writeErr(w, http.StatusNotFound, "checkin has no photo")
		return
	}

	member, err := s.rooms.IsMember(r.Context(), target.RoomID, userFrom(r.Context()).ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	if !member {
		writeErr(w, http.StatusForbidden, "room members only")
		return
	}

	photo, err := s.checkins.OpenPhoto(r.Context(), checkinID)
	if err != nil {
		s.mapError(w, err)
		return
	}

	contentType := photo.ContentType
	if !allowedPhotoTypes[contentType] {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	// never let a browser sniff these bytes into something executable
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, max-age=300")
	if _, err := io.Copy(w, photo.Body); err != nil {
		s.log.Error("streaming checkin photo failed", "checkin_id", checkinID, "err", err)
	}
}

// handleListCheckins godoc
//
//	@Summary		List room checkins
//	@Tags			checkins
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id		path		int		true	"room id"
//	@Param			status	query		string	false	"filter by status"	Enums(pending, approved, rejected, expired)
//	@Param			limit	query		int		false	"page size, default 20, max 100"
//	@Param			offset	query		int		false	"page offset"
//	@Success		200		{array}		checkin.Checkin
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		502		{object}	ErrorResponse
//	@Router			/rooms/{id}/checkins [get]
func (s *Server) handleListCheckins(w http.ResponseWriter, r *http.Request) {
	room, ok := s.membership(w, r)
	if !ok {
		return
	}

	status := pbv1.CheckinStatus_CHECKIN_STATUS_UNSPECIFIED
	if raw := r.URL.Query().Get("status"); raw != "" {
		st, ok := checkin.StatusFromName(raw)
		if !ok {
			writeErr(w, http.StatusBadRequest, "unknown status filter")
			return
		}
		status = st
	}
	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)
	if limit < 1 || limit > 100 || offset < 0 {
		writeErr(w, http.StatusBadRequest, "invalid pagination")
		return
	}

	list, err := s.checkins.List(r.Context(), room.ID, status, int32(limit), int32(offset))
	if err != nil {
		s.mapError(w, err)
		return
	}
	if list == nil {
		list = []checkin.Checkin{}
	}
	writeJSON(w, http.StatusOK, s.enrichBuddies(r, list))
}

// handleVote godoc
//
//	@Summary		Vote on a checkin
//	@Description	Approves or rejects a peer checkin. Only members of the checkin room can vote; voting for your own checkin is forbidden.
//	@Tags			checkins
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string		true	"checkin id"
//	@Param			body	body		VoteRequest	true	"vote"
//	@Success		200		{object}	checkin.Checkin
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		409		{object}	ErrorResponse
//	@Failure		502		{object}	ErrorResponse
//	@Router			/checkins/{id}/vote [post]
func (s *Server) handleVote(w http.ResponseWriter, r *http.Request) {
	checkinID := r.PathValue("id")
	if checkinID == "" {
		writeErr(w, http.StatusBadRequest, "invalid checkin id")
		return
	}
	var req VoteRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	target, err := s.checkins.Get(r.Context(), checkinID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	voter := userFrom(r.Context())
	if target.UserID == voter.ID {
		writeErr(w, http.StatusForbidden, "cannot vote for your own checkin")
		return
	}
	member, err := s.rooms.IsMember(r.Context(), target.RoomID, voter.ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	if !member {
		writeErr(w, http.StatusForbidden, "room members only")
		return
	}

	updated, err := s.checkins.Vote(r.Context(), checkinID, voter.ID, req.Approve)
	if err != nil {
		s.mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func queryInt(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return n
}
