package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
	"github.com/kosttiik/BuddyGym/core-service/internal/httpapi"
)

func TestGetUserProfile(t *testing.T) {
	e := newEnv()
	e.bearer(t, 2)
	e.users.achs[2] = []domain.Achievement{{Key: "first_checkin", GrantedAt: time.Now()}}

	rec := e.do(t, "GET", "/api/v1/users/2", nil, reqOpts{userID: 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("get user: %d %s", rec.Code, rec.Body.String())
	}
	resp := decode[httpapi.UserProfileResponse](t, rec)
	if resp.User.ID != 2 {
		t.Fatalf("unexpected profile: %+v", resp)
	}
	// the whole catalog comes back, locked entries included, each with its progress
	if len(resp.Achievements) != len(domain.Catalog) {
		t.Fatalf("got %d achievements, want the whole catalog (%d)",
			len(resp.Achievements), len(domain.Catalog))
	}
	first := resp.Achievements[0]
	if first.Key != domain.AchFirstCheckin || first.GrantedAt == nil {
		t.Errorf("first_checkin = %+v, want it granted", first)
	}
	if locked := resp.Achievements[1]; locked.GrantedAt != nil || locked.Target != 10 {
		t.Errorf("workouts_10 = %+v, want it locked with its target", locked)
	}

	if rec := e.do(t, "GET", "/api/v1/users/999", nil, reqOpts{userID: 1}); rec.Code != http.StatusNotFound {
		t.Fatalf("missing user: want 404 got %d", rec.Code)
	}
	if rec := e.do(t, "GET", "/api/v1/users/abc", nil, reqOpts{userID: 1}); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid id: want 400 got %d", rec.Code)
	}
}
