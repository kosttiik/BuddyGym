// Package domain holds core entities and reward rules.
package domain

import "time"

const (
	RoomOpen   = "open"
	RoomInvite = "invite"
)

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	// the telegram URL, unreachable for our users. Kept as the mirror change signal, not for display.
	PhotoURL  string    `json:"photo_url"`
	Theme     string    `json:"theme"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	// clients read the bytes from GET /users/{id}/avatar, never from object storage directly
	HasAvatar    bool   `json:"has_avatar"`
	AvatarKey    string `json:"-"`
	AvatarSource string `json:"-"`
}

type Achievement struct {
	Key       string    `json:"key"`
	GrantedAt time.Time `json:"granted_at"`
}

type Room struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	InviteCode    string    `json:"invite_code,omitempty"`
	GoalPerPeriod int       `json:"goal_per_period"`
	PeriodDays    int       `json:"period_days"`
	VotesRequired int       `json:"votes_required"`
	CreatorID     int64     `json:"creator_id"`
	CreatedAt     time.Time `json:"created_at"`
}

type RoomWithProgress struct {
	Room
	WorkoutsCount int `json:"workouts_count"`
	MembersCount  int `json:"members_count"`
	Streak        int `json:"streak"`
}

type Member struct {
	User
	WorkoutsCount int       `json:"workouts_count"`
	JoinedAt      time.Time `json:"joined_at"`
	Streak        int       `json:"streak"`
}
