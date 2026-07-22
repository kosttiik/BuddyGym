package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/kosttiik/BuddyGym/core-service/internal/checkin"
	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

const maxCommentPhoto = 5 << 20

type CommentRequest struct {
	Body string `json:"body" example:"Красавчик"`
}

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

func commentID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("commentId"), 10, 64)
	return id, err == nil && id > 0
}

// @Summary		List comments on a checkin
// @Tags			checkins
// @Security		BearerAuth
// @Produce		json
// @Param			id		path		string	true	"checkin id"
// @Param			limit	query		int		false	"page size, default 50, max 100"
// @Param			offset	query		int		false	"page offset"
// @Success		200		{array}		domain.Comment
// @Failure		401		{object}	ErrorResponse
// @Failure		403		{object}	ErrorResponse
// @Failure		404		{object}	ErrorResponse
// @Router			/checkins/{id}/comments [get]
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

	list, err := s.comments.List(r.Context(), target.ID, userFrom(r.Context()).ID, limit, offset)
	if err != nil {
		s.internal(w, err)
		return
	}
	if list == nil {
		list = []domain.Comment{}
	}
	writeJSON(w, http.StatusOK, list)
}

// @Summary		Comment on a checkin
// @Description	Room members only. Send JSON with a body, or multipart/form-data with a "body" field and an optional "photo" file up to 5 MB. Either the text or the photo must be there.
// @Tags			checkins
// @Security		BearerAuth
// @Accept			json
// @Accept			mpfd
// @Produce		json
// @Param			id		path		string			true	"checkin id"
// @Param			body	body		CommentRequest	false	"comment (json variant)"
// @Param			photo	formData	file			false	"attached image"
// @Success		201		{object}	domain.Comment
// @Failure		400		{object}	ErrorResponse
// @Failure		401		{object}	ErrorResponse
// @Failure		403		{object}	ErrorResponse
// @Failure		404		{object}	ErrorResponse
// @Router			/checkins/{id}/comments [post]
func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	target, ok := s.checkinRoom(w, r)
	if !ok {
		return
	}
	user := userFrom(r.Context())

	var rawBody string
	var photo []byte

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxCommentPhoto+1<<20)
		if err := r.ParseMultipartForm(maxCommentPhoto); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid multipart form")
			return
		}
		rawBody = r.FormValue("body")

		if file, _, err := r.FormFile("photo"); err == nil {
			defer file.Close()
			photo, err = io.ReadAll(io.LimitReader(file, maxCommentPhoto+1))
			if err != nil {
				s.internal(w, err)
				return
			}
			if len(photo) == 0 || len(photo) > maxCommentPhoto {
				writeErr(w, http.StatusBadRequest, "photo must be 1 byte .. 5 MB")
				return
			}
			if !isImage(photo) {
				writeErr(w, http.StatusBadRequest, "photo must be JPEG, PNG, GIF, WebP or HEIC")
				return
			}
		}
	} else {
		var req CommentRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		rawBody = req.Body
	}

	body, err := domain.NormalizeCommentBody(rawBody, len(photo) > 0)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	var photoKey string
	if len(photo) > 0 {
		if s.commentPhotos == nil {
			writeErr(w, http.StatusBadRequest, "photo comments are unavailable")
			return
		}
		photoKey = domain.CommentPhotoKey()
		if err := s.commentPhotos.Put(r.Context(), photoKey, photo); err != nil {
			s.internal(w, err)
			return
		}
	}

	comment, err := s.comments.Add(r.Context(), target.ID, target.RoomID, user.ID, body, photoKey)
	if err != nil {
		if photoKey != "" {
			s.commentPhotos.Delete(r.Context(), photoKey)
		}
		s.internal(w, err)
		return
	}
	s.emit(r.Context(), "comment.created", target.RoomID, user.ID, map[string]any{
		"checkin_id": target.ID,
		"comment_id": comment.ID,
		"body":       comment.Body,
		"has_photo":  comment.HasPhoto,
	})
	writeJSON(w, http.StatusCreated, comment)
}

// @Summary		Delete a comment
// @Description	The author can delete their own comment; the room creator can delete any.
// @Tags			checkins
// @Security		BearerAuth
// @Param			id			path	string	true	"checkin id"
// @Param			commentId	path	int		true	"comment id"
// @Success		204
// @Failure		400	{object}	ErrorResponse
// @Failure		401	{object}	ErrorResponse
// @Failure		403	{object}	ErrorResponse
// @Failure		404	{object}	ErrorResponse
// @Router			/checkins/{id}/comments/{commentId} [delete]
func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.checkinRoom(w, r); !ok {
		return
	}
	id, ok := commentID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid comment id")
		return
	}
	photoKey, err := s.comments.Delete(r.Context(), id, userFrom(r.Context()).ID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	if photoKey != "" && s.commentPhotos != nil {
		if err := s.commentPhotos.Delete(r.Context(), photoKey); err != nil {
			s.log.Error("delete comment photo", "err", err, "key", photoKey)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Like a comment
// @Description	Idempotent: liking twice leaves one like.
// @Tags			checkins
// @Security		BearerAuth
// @Produce		json
// @Param			id			path		string	true	"checkin id"
// @Param			commentId	path		int		true	"comment id"
// @Success		200			{object}	domain.Comment
// @Failure		401			{object}	ErrorResponse
// @Failure		403			{object}	ErrorResponse
// @Failure		404			{object}	ErrorResponse
// @Router			/checkins/{id}/comments/{commentId}/like [post]
func (s *Server) handleLikeComment(w http.ResponseWriter, r *http.Request) {
	s.setLike(w, r, true)
}

// @Summary		Remove a like from a comment
// @Tags			checkins
// @Security		BearerAuth
// @Produce		json
// @Param			id			path		string	true	"checkin id"
// @Param			commentId	path		int		true	"comment id"
// @Success		200			{object}	domain.Comment
// @Failure		401			{object}	ErrorResponse
// @Failure		403			{object}	ErrorResponse
// @Failure		404			{object}	ErrorResponse
// @Router			/checkins/{id}/comments/{commentId}/like [delete]
func (s *Server) handleUnlikeComment(w http.ResponseWriter, r *http.Request) {
	s.setLike(w, r, false)
}

func (s *Server) setLike(w http.ResponseWriter, r *http.Request, on bool) {
	if _, ok := s.checkinRoom(w, r); !ok {
		return
	}
	id, ok := commentID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid comment id")
		return
	}
	userID := userFrom(r.Context()).ID

	if _, err := s.comments.Get(r.Context(), id, userID); err != nil {
		s.mapError(w, err)
		return
	}

	var err error
	if on {
		err = s.comments.Like(r.Context(), id, userID)
	} else {
		err = s.comments.Unlike(r.Context(), id, userID)
	}
	if err != nil {
		s.internal(w, err)
		return
	}

	comment, err := s.comments.Get(r.Context(), id, userID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, comment)
}

// @Summary		Download a photo attached to a comment
// @Description	Room members only. The bucket is private and the bytes are proxied by core, so browsers must fetch this with XHR rather than a plain <img src>.
// @Tags			checkins
// @Security		BearerAuth
// @Produce		image/jpeg
// @Param			id			path		string	true	"checkin id"
// @Param			commentId	path		int		true	"comment id"
// @Success		200			{file}		binary
// @Failure		401			{object}	ErrorResponse
// @Failure		403			{object}	ErrorResponse
// @Failure		404			{object}	ErrorResponse
// @Router			/checkins/{id}/comments/{commentId}/photo [get]
func (s *Server) handleGetCommentPhoto(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.checkinRoom(w, r); !ok {
		return
	}
	id, ok := commentID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid comment id")
		return
	}
	comment, err := s.comments.Get(r.Context(), id, userFrom(r.Context()).ID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	if comment.PhotoKey == "" || s.commentPhotos == nil {
		writeErr(w, http.StatusNotFound, "comment has no photo")
		return
	}

	body, contentType, err := s.commentPhotos.Open(r.Context(), comment.PhotoKey)
	if err != nil {
		s.mapError(w, err)
		return
	}
	defer body.Close()

	if !allowedPhotoTypes[contentType] {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if _, err := io.Copy(w, body); err != nil {
		s.log.Error("streaming comment photo failed", "comment_id", id, "err", err)
	}
}

func (s *Server) enrichComments(r *http.Request, list []checkin.Checkin) []checkin.Checkin {
	if len(list) == 0 {
		return list
	}
	ids := make([]string, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	summaries, err := s.comments.Summaries(r.Context(), ids, userFrom(r.Context()).ID)
	if err != nil {
		s.log.Error("load comment summaries", "err", err)
		return list
	}
	for i := range list {
		summary := summaries[list[i].ID]
		list[i].CommentsCount = summary.Count
		list[i].TopComment = summary.Top
	}
	return list
}
