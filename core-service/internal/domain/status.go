package domain

import (
	"errors"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/rivo/uniseg"
)

const MaxStatusTextLen = 60

var (
	ErrStatusTextTooLong = errors.New("status text must be at most 60 characters")
	ErrStatusTextInvalid = errors.New("status text must not contain control characters")
	ErrStatusEmojiInvalid = errors.New("status emoji must be a single emoji")
)

// NormalizeStatusText trims the line and rejects anything that would break the layout.
// An empty result clears the status.
func NormalizeStatusText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if uniseg.GraphemeClusterCount(text) > MaxStatusTextLen {
		return "", ErrStatusTextTooLong
	}
	for _, r := range text {
		// a newline in a one-line chip would either be swallowed or blow up the row
		if unicode.IsControl(r) {
			return "", ErrStatusTextInvalid
		}
	}
	return text, nil
}

// NormalizeStatusEmoji accepts exactly one grapheme cluster that is not plain ASCII.
// Counting graphemes rather than runes is what makes a flag or a skin-toned emoji, which are
// several runes glued together, count as the single character the user actually typed.
func NormalizeStatusEmoji(emoji string) (string, error) {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return "", nil
	}
	if uniseg.GraphemeClusterCount(emoji) != 1 {
		return "", ErrStatusEmojiInvalid
	}
	for _, r := range emoji {
		if r < 0x80 || unicode.IsControl(r) {
			return "", ErrStatusEmojiInvalid
		}
	}
	return emoji, nil
}

const MaxCommentLen = 500

var (
	ErrCommentEmpty   = errors.New("comment must not be empty")
	ErrCommentTooLong = errors.New("comment must be at most 500 characters")
)

// NormalizeCommentBody allows an empty body when a photo carries the comment: a meme needs
// no caption.
func NormalizeCommentBody(body string, hasPhoto bool) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" && !hasPhoto {
		return "", ErrCommentEmpty
	}
	if uniseg.GraphemeClusterCount(body) > MaxCommentLen {
		return "", ErrCommentTooLong
	}
	return body, nil
}

func CommentPhotoKey() string {
	return "comments/" + uuid.NewString()
}
