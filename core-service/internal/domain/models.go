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
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	// the telegram URL, unreachable for our users. Kept as the mirror change signal, not for display.
	PhotoURL string `json:"photo_url"`
	Theme    string `json:"theme"`
	// derived from the workout total, not settable
	Rank string `json:"rank"`
	// what the member writes about themselves: a single emoji plus a short line
	StatusEmoji string    `json:"status_emoji"`
	StatusText  string    `json:"status_text"`
	CreatedAt   time.Time `json:"created_at"`
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
	// the object key stays server side; clients only learn whether there is a picture to fetch
	AvatarKey string `json:"-"`
	HasAvatar bool   `json:"has_avatar"`
}

// RoomAvatar is one picture in the room gallery. The newest one is what the room wears.
type RoomAvatar struct {
	ID         int64     `json:"id"`
	UploadedBy int64     `json:"uploaded_by"`
	CreatedAt  time.Time `json:"created_at"`
	IsCurrent  bool      `json:"is_current"`
	ObjectKey  string    `json:"-"`
}

// RoomAvatarKey is unique per upload: replacing a picture must not clobber the older ones.
func RoomAvatarKey(roomID int64) string {
	return "rooms/" + strconv.FormatInt(roomID, 10) + "/" + uuid.NewString()
}

type RoomWithProgress struct {
	Room
	WorkoutsCount int `json:"workouts_count"`
	MembersCount  int `json:"members_count"`
	Streak        int `json:"streak"`
	// when the current period closes and the streak burns unless the goal is met
	PeriodEndsAt time.Time `json:"period_ends_at"`
	// the viewer's personal goal in this room, falling back to the room goal
	MyGoal int `json:"my_goal"`
}

type Member struct {
	User
	WorkoutsCount int       `json:"workouts_count"`
	JoinedAt      time.Time `json:"joined_at"`
	Streak        int       `json:"streak"`
	PeriodEndsAt  time.Time `json:"period_ends_at"`
	// what the member trains and how often; empty/nil means the room defaults apply
	SportName     string `json:"sport_name"`
	SportEmoji    string `json:"sport_emoji"`
	GoalPerPeriod *int   `json:"goal_per_period"`
	EffectiveGoal int    `json:"effective_goal"`
}

type Comment struct {
	ID        int64  `json:"id"`
	CheckinID string `json:"checkin_id"`
	UserID    int64  `json:"user_id"`
	Author    User   `json:"author"`
	Body      string `json:"body"`
	// bytes come from GET /checkins/{id}/comments/{commentId}/photo, the bucket is private
	HasPhoto  bool      `json:"has_photo"`
	PhotoKey  string    `json:"-"`
	Likes     int       `json:"likes"`
	LikedByMe bool      `json:"liked_by_me"`
	CreatedAt time.Time `json:"created_at"`
}

// CommentSummary is what a checkin card needs: how many there are, and the one worth showing.
type CommentSummary struct {
	Count int      `json:"count"`
	Top   *Comment `json:"top,omitempty"`
}
