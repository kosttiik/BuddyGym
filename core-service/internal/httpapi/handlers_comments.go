package httpapi

import (
	"net/http"
	"strconv"

	"github.com/kosttiik/BuddyGym/core-service/internal/checkin"
	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type CommentRequest struct {
	Body string `json:"body" example:"Красавчик"`
}

// checkinRoom resolves the room a checkin belongs to and refuses anyone who is not in it.
// The photo handler already gates access this way; comments live behind the same door.
func (s *Server) checkinRoom(w http.ResponseWriter, r *http.Request) (checkin.Checkin, bool) {
	checkinID := r.PathValue("id")
	if checkinID == "" {
		writeErr(w, http.StatusBadRequest, "invalid checkin id")
		return checkin.Checkin{}, false
	}
	target, err := s.checkins.Get(r.Context(), checkinID)
	if err != nil {
		s.mapError(w, err)
		return checkin.Checkin{}, false
	}
	member, err := s.rooms.IsMember(r.Context(), target.RoomID, userFrom(r.Context()).ID)
	if err != nil {
		s.internal(w, err)
		return checkin.Checkin{}, false
	}
	if !member {
		writeErr(w, http.StatusForbidden, "room members only")
		return checkin.Checkin{}, false
	}
	return target, true
}

// handleListComments godoc
//
//	@Summary		List comments on a checkin
//	@Tags			checkins
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id		path		string	true	"checkin id"
//	@Param			limit	query		int		false	"page size, default 50, max 100"
//	@Param			offset	query		int		false	"page offset"
//	@Success		200		{array}		domain.Comment
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Router			/checkins/{id}/comments [get]
func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	target, ok := s.checkinRoom(w, r)
	if !ok {
		return
	}
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)
	if limit < 1 || limit > 100 || offset < 0 {
		writeErr(w, http.StatusBadRequest, "invalid pagination")
		return
	}

	list, err := s.comments.List(r.Context(), target.ID, limit, offset)
	if err != nil {
		s.internal(w, err)
		return
	}
	if list == nil {
		list = []domain.Comment{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleAddComment godoc
//
//	@Summary		Comment on a checkin
//	@Description	Room members only. The body is trimmed and must be 1..500 characters.
//	@Tags			checkins
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"checkin id"
//	@Param			body	body		CommentRequest	true	"comment"
//	@Success		201		{object}	domain.Comment
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Router			/checkins/{id}/comments [post]
func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	target, ok := s.checkinRoom(w, r)
	if !ok {
		return
	}
	var req CommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	body, err := domain.NormalizeComment(req.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	comment, err := s.comments.Add(r.Context(), target.ID, target.RoomID, userFrom(r.Context()).ID, body)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

// handleDeleteComment godoc
//
//	@Summary		Delete a comment
//	@Description	The author can delete their own comment; the room creator can delete any.
//	@Tags			checkins
//	@Security		BearerAuth
//	@Param			id			path	string	true	"checkin id"
//	@Param			commentId	path	int		true	"comment id"
//	@Success		204
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Router			/checkins/{id}/comments/{commentId} [delete]
func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.checkinRoom(w, r); !ok {
		return
	}
	commentID, err := strconv.ParseInt(r.PathValue("commentId"), 10, 64)
	if err != nil || commentID <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid comment id")
		return
	}
	if err := s.comments.Delete(r.Context(), commentID, userFrom(r.Context()).ID); err != nil {
		s.mapError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) enrichComments(r *http.Request, list []checkin.Checkin) []checkin.Checkin {
	if len(list) == 0 {
		return list
	}
	ids := make([]string, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	counts, err := s.comments.CountsFor(r.Context(), ids)
	if err != nil {
		// the feed is worth more than the counters
		s.log.Error("load comment counts", "err", err)
		return list
	}
	for i := range list {
		list[i].CommentsCount = counts[list[i].ID]
	}
	return list
}
