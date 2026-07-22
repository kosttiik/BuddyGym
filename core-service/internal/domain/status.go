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
	ErrStatusTextTooLong  = errors.New("status text must be at most 60 characters")
	ErrStatusTextInvalid  = errors.New("status text must not contain control characters")
	ErrStatusEmojiInvalid = errors.New("status emoji must be a single emoji")
)

func NormalizeStatusText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if uniseg.GraphemeClusterCount(text) > MaxStatusTextLen {
		return "", ErrStatusTextTooLong
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return "", ErrStatusTextInvalid
		}
	}
	return text, nil
}

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
