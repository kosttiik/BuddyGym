package httpapi

import (
	"testing"
	"time"
)

func TestParseTZOffset(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"", 0},
		{"-180", -180 * time.Minute}, // MSK
		{"240", 240 * time.Minute},
		{"0", 0},
		{"garbage", 0},
		{"9999", 0},  // out of range, ignored
		{"-9999", 0}, // out of range, ignored
	}
	for _, c := range cases {
		if got := parseTZOffset(c.raw); got != c.want {
			t.Errorf("parseTZOffset(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}
