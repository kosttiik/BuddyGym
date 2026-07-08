// Package checkin wraps the gRPC client for checkin-service.
package checkin

import (
	"context"
	"time"

	"google.golang.org/grpc"

	pbv1 "github.com/kosttiik/BuddyGym/core-service/internal/pb/buddygym/v1"
)

type Geo struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type Checkin struct {
	ID            string    `json:"id"`
	RoomID        int64     `json:"room_id"`
	UserID        int64     `json:"user_id"`
	Status        string    `json:"status" enums:"pending,approved,rejected,expired"`
	PhotoURL      string    `json:"photo_url,omitempty"`
	Geo           *Geo      `json:"geo,omitempty"`
	VotesApprove  int32     `json:"votes_approve"`
	VotesReject   int32     `json:"votes_reject"`
	VotesRequired int32     `json:"votes_required"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

var statusNames = map[pbv1.CheckinStatus]string{
	pbv1.CheckinStatus_CHECKIN_STATUS_PENDING:  "pending",
	pbv1.CheckinStatus_CHECKIN_STATUS_APPROVED: "approved",
	pbv1.CheckinStatus_CHECKIN_STATUS_REJECTED: "rejected",
	pbv1.CheckinStatus_CHECKIN_STATUS_EXPIRED:  "expired",
}

func StatusFromName(name string) (pbv1.CheckinStatus, bool) {
	for st, n := range statusNames {
		if n == name {
			return st, true
		}
	}
	return pbv1.CheckinStatus_CHECKIN_STATUS_UNSPECIFIED, false
}

func fromPB(c *pbv1.Checkin) Checkin {
	if c == nil {
		return Checkin{}
	}
	out := Checkin{
		ID:            c.Id,
		RoomID:        c.RoomId,
		UserID:        c.UserId,
		Status:        statusNames[c.Status],
		PhotoURL:      c.PhotoUrl,
		VotesApprove:  c.VotesApprove,
		VotesReject:   c.VotesReject,
		VotesRequired: c.VotesRequired,
	}
	if c.Geo != nil {
		out.Geo = &Geo{Lat: c.Geo.Lat, Lon: c.Geo.Lon}
	}
	if c.CreatedAt != nil {
		out.CreatedAt = c.CreatedAt.AsTime()
	}
	if c.ExpiresAt != nil {
		out.ExpiresAt = c.ExpiresAt.AsTime()
	}
	return out
}

type Client struct {
	api pbv1.CheckinServiceClient
}

func NewClient(conn grpc.ClientConnInterface) *Client {
	return &Client{api: pbv1.NewCheckinServiceClient(conn)}
}

func (c *Client) Create(ctx context.Context, roomID, userID int64, votesRequired int32, photo []byte, geo *Geo) (Checkin, error) {
	req := &pbv1.CreateCheckinRequest{
		RoomId:        roomID,
		UserId:        userID,
		VotesRequired: votesRequired,
	}
	if geo != nil {
		req.Proof = &pbv1.CreateCheckinRequest_Geo{Geo: &pbv1.GeoPoint{Lat: geo.Lat, Lon: geo.Lon}}
	} else {
		req.Proof = &pbv1.CreateCheckinRequest_Photo{Photo: photo}
	}
	resp, err := c.api.CreateCheckin(ctx, req)
	if err != nil {
		return Checkin{}, err
	}
	return fromPB(resp.GetCheckin()), nil
}

func (c *Client) Get(ctx context.Context, id string) (Checkin, error) {
	resp, err := c.api.GetCheckin(ctx, &pbv1.GetCheckinRequest{CheckinId: id})
	if err != nil {
		return Checkin{}, err
	}
	return fromPB(resp.GetCheckin()), nil
}

func (c *Client) List(ctx context.Context, roomID int64, status pbv1.CheckinStatus, limit, offset int32) ([]Checkin, error) {
	resp, err := c.api.ListRoomCheckins(ctx, &pbv1.ListRoomCheckinsRequest{
		RoomId: roomID, Status: status, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Checkin, 0, len(resp.GetCheckins()))
	for _, c := range resp.GetCheckins() {
		out = append(out, fromPB(c))
	}
	return out, nil
}

func (c *Client) Vote(ctx context.Context, checkinID string, voterID int64, approve bool) (Checkin, error) {
	resp, err := c.api.CastVote(ctx, &pbv1.CastVoteRequest{
		CheckinId: checkinID, VoterId: voterID, Approve: approve,
	})
	if err != nil {
		return Checkin{}, err
	}
	return fromPB(resp.GetCheckin()), nil
}
