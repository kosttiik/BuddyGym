package domain

import (
	"strings"
	"testing"
)

func TestNormalizeStatusEmoji(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  string
		valid bool
	}{
		{"empty clears the status", "", "", true},
		{"a plain emoji", "💪", "💪", true},
		// a skin-toned emoji is several runes glued into one grapheme, and it is what a user types
		{"a skin toned emoji is still one character", "💪🏽", "💪🏽", true},
		{"a flag is one character", "🇷🇺", "🇷🇺", true},
		{"surrounding spaces are trimmed", "  🔥 ", "🔥", true},
		{"two emoji are not one", "💪🔥", "", false},
		{"a letter is not an emoji", "x", "", false},
		{"a digit is not an emoji", "7", "", false},
		{"a word is not an emoji", "beast", "", false},
	}
	for _, tt := range tests {
		got, err := NormalizeStatusEmoji(tt.in)
		if tt.valid && err != nil {
			t.Errorf("%s: NormalizeStatusEmoji(%q) = %v", tt.name, tt.in, err)
			continue
		}
		if !tt.valid && err == nil {
			t.Errorf("%s: NormalizeStatusEmoji(%q) accepted it", tt.name, tt.in)
			continue
		}
		if tt.valid && got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestNormalizeStatusText(t *testing.T) {
	if got, err := NormalizeStatusText("  На массе  "); err != nil || got != "На массе" {
		t.Errorf("trim: got %q, %v", got, err)
	}
	if got, err := NormalizeStatusText(""); err != nil || got != "" {
		t.Errorf("empty clears the status: got %q, %v", got, err)
	}
	if _, err := NormalizeStatusText(strings.Repeat("я", MaxStatusTextLen+1)); err == nil {
		t.Error("an over-long line was accepted")
	}
	// counted in graphemes, so a 60-emoji line is 60 characters and not 240 bytes
	if _, err := NormalizeStatusText(strings.Repeat("я", MaxStatusTextLen)); err != nil {
		t.Errorf("a line at the limit was rejected: %v", err)
	}
	if _, err := NormalizeStatusText("две\nстроки"); err == nil {
		t.Error("a newline was accepted into a one-line chip")
	}
}
