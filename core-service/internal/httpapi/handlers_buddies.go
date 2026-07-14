package httpapi

import (
	"net/http"
	"strconv"

	"github.com/kosttiik/BuddyGym/core-service/internal/checkin"
	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

const maxBuddies = 10

type BuddiesRequest struct {
	UserIDs []int64 `json:"user_ids"`
}

// A buddy who is not in this room is dropped rather than failing the whole request: tagging a
// partner who shares one of your three rooms should still credit them in that one.
func (s *Server) resolveBuddies(w http.ResponseWriter, r *http.Request, roomID int64, userIDs []int64) ([]int64, bool) {
	author := userFrom(r.Context()).ID
	if len(userIDs) > maxBuddies {
		writeErr(w, http.StatusBadRequest, "at most "+strconv.Itoa(maxBuddies)+" buddies")
		return nil, false
	}

	seen := make(map[int64]struct{}, len(userIDs))
	out := make([]int64, 0, len(userIDs))
	for _, id := range userIDs {
		if id == author {
			writeErr(w, http.StatusBadRequest, "cannot tag yourself")
			return nil, false
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}

		member, err := s.rooms.IsMember(r.Context(), roomID, id)
		if err != nil {
			s.internal(w, err)
			return nil, false
		}
		if member {
			out = append(out, id)
		}
	}
	return out, true
}

// handleAddBuddies godoc
//
//	@Summary		Tag people who trained with you
//	@Description	Adds buddies to a checkin that is still being voted on. Only the author can tag, and buddies who are not members of the checkin room are ignored. The workout is credited to them once the room approves the checkin, so a rejected photo hands out nothing.
//	@Tags			checkins
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"checkin id"
//	@Param			body	body		BuddiesRequest	true	"tagged user ids"
//	@Success		200		{array}		domain.User
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		409		{object}	ErrorResponse
//	@Router			/checkins/{id}/buddies [post]
func (s *Server) handleAddBuddies(w http.ResponseWriter, r *http.Request) {
	checkinID := r.PathValue("id")
	if checkinID == "" {
		writeErr(w, http.StatusBadRequest, "invalid checkin id")
		return
	}
	var req BuddiesRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	target, err := s.checkins.Get(r.Context(), checkinID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	author := userFrom(r.Context())
	if target.UserID != author.ID {
		writeErr(w, http.StatusForbidden, "only the author can tag buddies")
		return
	}
	if target.Status != "pending" {
		writeErr(w, http.StatusConflict, "checkin is no longer open for tagging")
		return
	}

	buddyIDs, ok := s.resolveBuddies(w, r, target.RoomID, req.UserIDs)
	if !ok {
		return
	}
	if err := s.buddies.Tag(r.Context(), checkinID, target.RoomID, author.ID, buddyIDs); err != nil {
		s.internal(w, err)
		return
	}

	s.writeBuddies(w, r, checkinID)
}

// handleRemoveBuddy godoc
//
//	@Summary		Remove a tagged buddy
//	@Description	Untags a buddy from a checkin. Only the author can untag, and only while the checkin is still pending: once it is approved the workout is already theirs.
//	@Tags			checkins
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id		path		string	true	"checkin id"
//	@Param			userId	path		int		true	"tagged user id"
//	@Success		200		{array}		domain.User
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		409		{object}	ErrorResponse
//	@Router			/checkins/{id}/buddies/{userId} [delete]
func (s *Server) handleRemoveBuddy(w http.ResponseWriter, r *http.Request) {
	checkinID := r.PathValue("id")
	buddyID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if checkinID == "" || err != nil || buddyID <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid checkin or user id")
		return
	}

	target, err := s.checkins.Get(r.Context(), checkinID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	if target.UserID != userFrom(r.Context()).ID {
		writeErr(w, http.StatusForbidden, "only the author can untag buddies")
		return
	}
	if target.Status != "pending" {
		writeErr(w, http.StatusConflict, "checkin is already settled")
		return
	}
	if err := s.buddies.Untag(r.Context(), checkinID, buddyID); err != nil {
		s.mapError(w, err)
		return
	}
	s.writeBuddies(w, r, checkinID)
}

func (s *Server) writeBuddies(w http.ResponseWriter, r *http.Request, checkinID string) {
	byCheckin, err := s.buddies.ForCheckins(r.Context(), []string{checkinID})
	if err != nil {
		s.internal(w, err)
		return
	}
	users := byCheckin[checkinID]
	if users == nil {
		users = []domain.User{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) enrichBuddies(r *http.Request, list []checkin.Checkin) []checkin.Checkin {
	if len(list) == 0 {
		return list
	}
	ids := make([]string, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	byCheckin, err := s.buddies.ForCheckins(r.Context(), ids)
	if err != nil {
		// the feed is worth more than the tags: serve the checkins without them
		s.log.Error("load buddies", "err", err)
		return list
	}
	for i := range list {
		list[i].Buddies = byCheckin[list[i].ID]
	}
	return list
}
