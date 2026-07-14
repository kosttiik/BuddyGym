// Package checkin wraps the gRPC client for checkin-service.
package checkin

import (
	"context"
	"io"
	"time"

	"google.golang.org/grpc"

	pbv1 "github.com/kosttiik/BuddyGym/core-service/internal/pb/buddygym/v1"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type Geo struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Target is one room a proof is submitted to. Quorum is per room.
type Target struct {
	RoomID        int64
	VotesRequired int32
}

type Checkin struct {
	ID     string `json:"id"`
	RoomID int64  `json:"room_id"`
	UserID int64  `json:"user_id"`
	Status string `json:"status" enums:"pending,approved,rejected,expired"`
	// the storage key never leaves core; clients fetch bytes from /checkins/{id}/photo
	HasPhoto bool `json:"has_photo"`
	// photos are purged after a retention window; once purged the bytes are gone for good
	PhotoPurged    bool       `json:"photo_purged"`
	PhotoExpiresAt *time.Time `json:"photo_expires_at,omitempty"`
	Geo            *Geo       `json:"geo,omitempty"`
	// people the author tagged as training with them; filled in by core, not checkin-service
	Buddies        []domain.User `json:"buddies,omitempty"`
	VotesApprove   int32         `json:"votes_approve"`
	VotesReject    int32      `json:"votes_reject"`
	VotesRequired  int32      `json:"votes_required"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
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
		HasPhoto:      c.PhotoKey != "" && !c.PhotoPurged,
		PhotoPurged:   c.PhotoPurged,
		VotesApprove:  c.VotesApprove,
		VotesReject:   c.VotesReject,
		VotesRequired: c.VotesRequired,
	}
	if c.PhotoExpiresAt != nil {
		at := c.PhotoExpiresAt.AsTime()
		out.PhotoExpiresAt = &at
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

// Create submits one proof to every target room. A photo is uploaded once and
// shared by all of the checkins it produces.
func (c *Client) Create(ctx context.Context, userID int64, targets []Target, photo []byte, geo *Geo) ([]Checkin, error) {
	req := &pbv1.CreateCheckinRequest{UserId: userID}
	for _, t := range targets {
		req.Targets = append(req.Targets, &pbv1.CheckinTarget{
			RoomId:        t.RoomID,
			VotesRequired: t.VotesRequired,
		})
	}
	if geo != nil {
		req.Proof = &pbv1.CreateCheckinRequest_Geo{Geo: &pbv1.GeoPoint{Lat: geo.Lat, Lon: geo.Lon}}
	} else {
		req.Proof = &pbv1.CreateCheckinRequest_Photo{Photo: photo}
	}
	resp, err := c.api.CreateCheckin(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make([]Checkin, 0, len(resp.GetCheckins()))
	for _, item := range resp.GetCheckins() {
		out = append(out, fromPB(item))
	}
	return out, nil
}

// Photo is the decoded head of a photo stream: content type plus the bytes reader.
type Photo struct {
	ContentType string
	Body        io.Reader
}

// OpenPhoto starts the server stream and reads the first chunk, which carries the
// content type. The caller must have already authorized access to the checkin.
func (c *Client) OpenPhoto(ctx context.Context, checkinID string) (Photo, error) {
	stream, err := c.api.GetCheckinPhoto(ctx, &pbv1.GetCheckinPhotoRequest{CheckinId: checkinID})
	if err != nil {
		return Photo{}, err
	}
	first, err := stream.Recv()
	if err != nil {
		return Photo{}, err
	}
	return Photo{
		ContentType: first.GetContentType(),
		Body:        &photoReader{stream: stream, buf: first.GetData()},
	}, nil
}

type photoReader struct {
	stream grpc.ServerStreamingClient[pbv1.CheckinPhotoChunk]
	buf    []byte
	err    error
}

func (r *photoReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		chunk, err := r.stream.Recv()
		if err != nil {
			r.err = err
			return 0, err
		}
		r.buf = chunk.GetData()
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
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

// PurgeRoom removes every checkin of a deleted room. Photos shared with a room that is
// still alive are kept.
func (c *Client) PurgeRoom(ctx context.Context, roomID int64) error {
	_, err := c.api.PurgeRoom(ctx, &pbv1.PurgeRoomRequest{RoomId: roomID})
	return err
}
