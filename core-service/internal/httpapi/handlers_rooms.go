package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type CreateRoomRequest struct {
	Name          string `json:"name" example:"Iron buddies"`
	Kind          string `json:"kind" example:"open" enums:"open,invite"`
	GoalPerPeriod int    `json:"goal_per_period" example:"3" minimum:"1" maximum:"100"`
	PeriodDays    int    `json:"period_days" example:"7" minimum:"1" maximum:"90"`
	VotesRequired int    `json:"votes_required" example:"2" minimum:"1" maximum:"20"`
}

type JoinByCodeRequest struct {
	InviteCode string `json:"invite_code" example:"7HKPQ2XW"`
}

type RoomDetailResponse struct {
	Room    domain.Room     `json:"room"`
	Members []domain.Member `json:"members"`
}

func (req *CreateRoomRequest) validate() string {
	req.Name = strings.TrimSpace(req.Name)
	switch {
	case req.Name == "" || len(req.Name) > 64:
		return "name must be 1..64 chars"
	case req.Kind != domain.RoomOpen && req.Kind != domain.RoomInvite:
		return "kind must be open or invite"
	case req.GoalPerPeriod < 1 || req.GoalPerPeriod > 100:
		return "goal_per_period must be 1..100"
	case req.PeriodDays < 1 || req.PeriodDays > 90:
		return "period_days must be 1..90"
	case req.VotesRequired < 1 || req.VotesRequired > 20:
		return "votes_required must be 1..20"
	}
	return ""
}

func roomID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// membership loads the room and checks the current user is in it.
func (s *Server) membership(w http.ResponseWriter, r *http.Request) (domain.Room, bool) {
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
	member, err := s.rooms.IsMember(r.Context(), room.ID, userFrom(r.Context()).ID)
	if err != nil {
		s.internal(w, err)
		return domain.Room{}, false
	}
	if !member {
		writeErr(w, http.StatusForbidden, "room members only")
		return domain.Room{}, false
	}
	return room, true
}

// handleCreateRoom godoc
//
//	@Summary		Create a room
//	@Description	Creates a room and enrolls the creator. Returns the room with its invite code.
//	@Tags			rooms
//	@Security		TmaAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CreateRoomRequest	true	"room settings"
//	@Success		201		{object}	domain.Room
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/rooms [post]
func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	var req CreateRoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	room, err := s.rooms.Create(r.Context(), domain.Room{
		Name:          req.Name,
		Kind:          req.Kind,
		GoalPerPeriod: req.GoalPerPeriod,
		PeriodDays:    req.PeriodDays,
		VotesRequired: req.VotesRequired,
		CreatorID:     userFrom(r.Context()).ID,
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, room)
}

// handleListRooms godoc
//
//	@Summary		List my rooms
//	@Description	Rooms the user belongs to, with the current period workout counter.
//	@Tags			rooms
//	@Security		TmaAuth
//	@Produce		json
//	@Success		200	{array}		domain.RoomWithProgress
//	@Failure		401	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/rooms [get]
func (s *Server) handleListRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := s.rooms.ListByUser(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	if rooms == nil {
		rooms = []domain.RoomWithProgress{}
	}
	writeJSON(w, http.StatusOK, rooms)
}

// handleGetRoom godoc
//
//	@Summary		Get room details
//	@Description	Room with members. Invite-only rooms are visible to members only; the invite code is hidden from non-members.
//	@Tags			rooms
//	@Security		TmaAuth
//	@Produce		json
//	@Param			id	path		int	true	"room id"
//	@Success		200	{object}	RoomDetailResponse
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/rooms/{id} [get]
func (s *Server) handleGetRoom(w http.ResponseWriter, r *http.Request) {
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
	member, err := s.rooms.IsMember(r.Context(), room.ID, userFrom(r.Context()).ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	if !member {
		if room.Kind == domain.RoomInvite {
			writeErr(w, http.StatusForbidden, "room members only")
			return
		}
		room.InviteCode = ""
	}
	members, err := s.rooms.Members(r.Context(), room.ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, RoomDetailResponse{Room: room, Members: members})
}

// handleJoinRoom godoc
//
//	@Summary		Join an open room
//	@Description	Joins an open room by id. Invite-only rooms require POST /rooms/join with a code. Idempotent.
//	@Tags			rooms
//	@Security		TmaAuth
//	@Produce		json
//	@Param			id	path		int	true	"room id"
//	@Success		200	{object}	domain.Room
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/rooms/{id}/join [post]
func (s *Server) handleJoinRoom(w http.ResponseWriter, r *http.Request) {
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
	if room.Kind != domain.RoomOpen {
		writeErr(w, http.StatusForbidden, "room is invite-only")
		return
	}
	if err := s.rooms.Join(r.Context(), room.ID, userFrom(r.Context()).ID); err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, room)
}

// handleJoinByCode godoc
//
//	@Summary		Join a room by invite code
//	@Description	Joins any room using its invite code. Idempotent.
//	@Tags			rooms
//	@Security		TmaAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		JoinByCodeRequest	true	"invite code"
//	@Success		200		{object}	domain.Room
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/rooms/join [post]
func (s *Server) handleJoinByCode(w http.ResponseWriter, r *http.Request) {
	var req JoinByCodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.InviteCode = strings.ToUpper(strings.TrimSpace(req.InviteCode))
	if req.InviteCode == "" {
		writeErr(w, http.StatusBadRequest, "invite_code is required")
		return
	}
	room, err := s.rooms.GetByInvite(r.Context(), req.InviteCode)
	if err != nil {
		s.mapError(w, err)
		return
	}
	if err := s.rooms.Join(r.Context(), room.ID, userFrom(r.Context()).ID); err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, room)
}

// handleLeaveRoom godoc
//
//	@Summary		Leave a room
//	@Tags			rooms
//	@Security		TmaAuth
//	@Produce		json
//	@Param			id	path	int	true	"room id"
//	@Success		204
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/rooms/{id}/leave [post]
func (s *Server) handleLeaveRoom(w http.ResponseWriter, r *http.Request) {
	id, ok := roomID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid room id")
		return
	}
	if err := s.rooms.Leave(r.Context(), id, userFrom(r.Context()).ID); err != nil {
		s.mapError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
