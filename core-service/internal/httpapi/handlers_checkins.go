package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	pbv1 "github.com/kosttiik/BuddyGym/core-service/internal/pb/buddygym/v1"

	"github.com/kosttiik/BuddyGym/core-service/internal/checkin"
)

const maxPhotoSize = 10 << 20

type CreateCheckinGeoRequest struct {
	Geo checkin.Geo `json:"geo"`
}

type VoteRequest struct {
	Approve bool `json:"approve" example:"true"`
}

// handleCreateCheckin godoc
//
//	@Summary		Create a checkin
//	@Description	Submits workout proof to checkin-service. Send multipart/form-data with a "photo" file (up to 10 MB), or JSON with a geo point for the fast path.
//	@Tags			checkins
//	@Security		TmaAuth
//	@Accept			mpfd
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int						true	"room id"
//	@Param			photo	formData	file					false	"workout photo"
//	@Param			body	body		CreateCheckinGeoRequest	false	"geo proof (json variant)"
//	@Success		201		{object}	checkin.Checkin
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		502		{object}	ErrorResponse
//	@Router			/rooms/{id}/checkins [post]
func (s *Server) handleCreateCheckin(w http.ResponseWriter, r *http.Request) {
	room, ok := s.membership(w, r)
	if !ok {
		return
	}

	var photo []byte
	var geo *checkin.Geo

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
	}

	created, err := s.checkins.Create(r.Context(), room.ID, userFrom(r.Context()).ID,
		int32(room.VotesRequired), photo, geo)
	if err != nil {
		s.mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handleListCheckins godoc
//
//	@Summary		List room checkins
//	@Tags			checkins
//	@Security		TmaAuth
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
	writeJSON(w, http.StatusOK, list)
}

// handleVote godoc
//
//	@Summary		Vote on a checkin
//	@Description	Approves or rejects a peer checkin. Only members of the checkin room can vote; voting for your own checkin is forbidden.
//	@Tags			checkins
//	@Security		TmaAuth
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
