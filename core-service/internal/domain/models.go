// Package domain holds core entities and reward rules.
package domain

import (
	"strconv"
	"time"

	"github.com/google/uuid"
)

const (
	RoomOpen   = "open"
	RoomInvite = "invite"
)

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	FirstName    string    `json:"first_name"`
	PhotoURL     string    `json:"photo_url"`
	Theme        string    `json:"theme"`
	Rank         string    `json:"rank"`
	StatusEmoji  string    `json:"status_emoji"`
	StatusText   string    `json:"status_text"`
	CreatedAt    time.Time `json:"created_at"`
	HasAvatar    bool      `json:"has_avatar"`
	AvatarKey    string    `json:"-"`
	AvatarSource string    `json:"-"`
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
	AvatarKey     string    `json:"-"`
	HasAvatar     bool      `json:"has_avatar"`
}

type RoomAvatar struct {
	ID         int64     `json:"id"`
	UploadedBy int64     `json:"uploaded_by"`
	CreatedAt  time.Time `json:"created_at"`
	IsCurrent  bool      `json:"is_current"`
	ObjectKey  string    `json:"-"`
}

func RoomAvatarKey(roomID int64) string {
	return "rooms/" + strconv.FormatInt(roomID, 10) + "/" + uuid.NewString()
}

type RoomWithProgress struct {
	Room
	WorkoutsCount int       `json:"workouts_count"`
	MembersCount  int       `json:"members_count"`
	Streak        int       `json:"streak"`
	PeriodEndsAt  time.Time `json:"period_ends_at"`
	MyGoal        int       `json:"my_goal"`
}

type Member struct {
	User
	WorkoutsCount          int        `json:"workouts_count"`
	JoinedAt               time.Time  `json:"joined_at"`
	Streak                 int        `json:"streak"`
	PeriodEndsAt           time.Time  `json:"period_ends_at"`
	SportName              string     `json:"sport_name"`
	SportEmoji             string     `json:"sport_emoji"`
	GoalPerPeriod          *int       `json:"goal_per_period"`
	EffectiveGoal          int        `json:"effective_goal"`
	HasClosedPeriod        bool       `json:"has_closed_period"`
	LastClosedPeriodFailed bool       `json:"last_closed_period_failed"`
	Freeze                 *Freeze    `json:"freeze,omitempty"`
	FreezeCooldownUntil    *time.Time `json:"freeze_cooldown_until,omitempty"`
}

type Comment struct {
	ID        int64     `json:"id"`
	CheckinID string    `json:"checkin_id"`
	UserID    int64     `json:"user_id"`
	Author    User      `json:"author"`
	Body      string    `json:"body"`
	HasPhoto  bool      `json:"has_photo"`
	PhotoKey  string    `json:"-"`
	Likes     int       `json:"likes"`
	LikedByMe bool      `json:"liked_by_me"`
	CreatedAt time.Time `json:"created_at"`
	ReplyTo   *int64    `json:"reply_to,omitempty"`
	// the quoted line the reply answers, resolved so the client needs no second lookup
	ReplyToAuthor string `json:"reply_to_author,omitempty"`
	ReplyToBody   string `json:"reply_to_body,omitempty"`
}

type CommentSummary struct {
	Count int      `json:"count"`
	Top   *Comment `json:"top,omitempty"`
}
