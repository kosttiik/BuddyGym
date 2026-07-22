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

type UpdateRoomRequest = CreateRoomRequest

type JoinByCodeRequest struct {
	InviteCode string `json:"invite_code" example:"7HKPQ2XW"`
}

// UpdateMembershipRequest replaces the member's personal settings in a room.
// A null goal_per_period falls back to the room goal; empty strings clear the sport.
type UpdateMembershipRequest struct {
	SportName     string `json:"sport_name" example:"climbing"`
	SportEmoji    string `json:"sport_emoji" example:"🧗"`
	GoalPerPeriod *int   `json:"goal_per_period" example:"2" minimum:"1" maximum:"100"`
}

func (req *UpdateMembershipRequest) validate() string {
	req.SportName = strings.TrimSpace(req.SportName)
	switch {
	case len(req.SportName) > 32:
		return "sport_name must be at most 32 chars"
	case req.SportEmoji != "" && !domain.IsSportEmoji(req.SportEmoji):
		return "sport_emoji must be a sport emoji"
	case req.GoalPerPeriod != nil && (*req.GoalPerPeriod < 1 || *req.GoalPerPeriod > 100):
		return "goal_per_period must be 1..100"
	}
	return ""
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
//	@Security		BearerAuth
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

// handleUpdateRoom godoc
//
//	@Summary		Update a room
//	@Description	Updates room settings. Only the creator may do this.
//	@Tags			rooms
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path	int				true	"room id"
//	@Param			body	body	UpdateRoomRequest	true	"room settings"
//	@Success		200	{object}	domain.Room
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/rooms/{id} [patch]
func (s *Server) handleUpdateRoom(w http.ResponseWriter, r *http.Request) {
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
	if room.CreatorID != userFrom(r.Context()).ID {
		writeErr(w, http.StatusForbidden, "room creator only")
		return
	}
	var req UpdateRoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	room.Name, room.Kind = req.Name, req.Kind
	room.GoalPerPeriod, room.PeriodDays, room.VotesRequired = req.GoalPerPeriod, req.PeriodDays, req.VotesRequired
	updated, err := s.rooms.Update(r.Context(), room)
	if err != nil {
		s.mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleUpdateMembership godoc
//
//	@Summary		Update my settings in a room
//	@Description	Sets the member's personal sport and workout goal for this room. Null goal falls back to the room goal, empty strings clear the sport.
//	@Tags			rooms
//	@Security		BearerAuth
//	@Accept			json
//	@Param			id		path	int							true	"room id"
//	@Param			body	body	UpdateMembershipRequest	true	"personal settings"
//	@Success		204
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/rooms/{id}/membership [patch]
func (s *Server) handleUpdateMembership(w http.ResponseWriter, r *http.Request) {
	room, ok := s.membership(w, r)
	if !ok {
		return
	}
	var req UpdateMembershipRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.rooms.UpdateMembership(r.Context(), room.ID, userFrom(r.Context()).ID,
		req.SportName, req.SportEmoji, req.GoalPerPeriod); err != nil {
		s.mapError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteRoom godoc
//
//	@Summary		Delete a room
//	@Description	Deletes a room and its memberships. Only the creator may do this.
//	@Tags			rooms
//	@Security		BearerAuth
//	@Param			id	path	int	true	"room id"
//	@Success		204
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/rooms/{id} [delete]
func (s *Server) handleDeleteRoom(w http.ResponseWriter, r *http.Request) {
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
	if room.CreatorID != userFrom(r.Context()).ID {
		writeErr(w, http.StatusForbidden, "room creator only")
		return
	}
	if err := s.rooms.Delete(r.Context(), room.ID); err != nil {
		s.mapError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListRooms godoc
//
//	@Summary		List my rooms
//	@Description	Rooms the user belongs to, with the current period workout counter.
//	@Tags			rooms
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{array}		domain.RoomWithProgress
//	@Failure		401	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/rooms [get]
func (s *Server) handleListRooms(w http.ResponseWriter, r *http.Request) {
	userID := userFrom(r.Context()).ID
	rooms, err := s.rooms.ListByUser(r.Context(), userID)
	if err != nil {
		s.internal(w, err)
		return
	}
	if rooms == nil {
		rooms = []domain.RoomWithProgress{}
	}

	streaks, err := s.streaks.StreaksByUser(r.Context(), userID)
	if err != nil {
		s.internal(w, err)
		return
	}
	byRoom := make(map[int64]int, len(streaks))
	for _, in := range streaks {
		byRoom[in.RoomID] = in.Streak(s.now())
	}
	for i := range rooms {
		rooms[i].Streak = byRoom[rooms[i].ID]
	}
	writeJSON(w, http.StatusOK, rooms)
}

// handleListOpenRooms godoc
//
//	@Summary		List open rooms
//	@Description	Rooms anyone can join, excluding the ones the user is already in. Invite codes are not returned.
//	@Tags			rooms
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{array}		domain.Room
//	@Failure		401	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/rooms/open [get]
func (s *Server) handleListOpenRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := s.rooms.ListOpen(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	if rooms == nil {
		rooms = []domain.Room{}
	}
	writeJSON(w, http.StatusOK, rooms)
}

// handleGetRoom godoc
//
//	@Summary		Get room details
//	@Description	Room with members. Invite-only rooms are visible to members only; the invite code is hidden from non-members.
//	@Tags			rooms
//	@Security		BearerAuth
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
	streaks, err := s.streaks.StreaksByRoom(r.Context(), room.ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	byUser := make(map[int64]int, len(streaks))
	for _, in := range streaks {
		byUser[in.UserID] = in.Streak(s.now())
	}
	for i := range members {
		members[i].Streak = byUser[members[i].ID]
	}
	writeJSON(w, http.StatusOK, RoomDetailResponse{Room: room, Members: members})
}

// handleJoinRoom godoc
//
//	@Summary		Join an open room
//	@Description	Joins an open room by id. Invite-only rooms require POST /rooms/join with a code. Idempotent.
//	@Tags			rooms
//	@Security		BearerAuth
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
//	@Security		BearerAuth
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
//	@Security		BearerAuth
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
