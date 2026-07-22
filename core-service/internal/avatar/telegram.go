// Package avatar mirrors Telegram profile pictures into object storage.
package avatar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	apiBase  = "https://api.telegram.org"
	maxBytes = 5 << 20
)

var ErrNoPhoto = fmt.Errorf("no telegram profile photo")

type Telegram struct {
	base  string
	token string
	http  *http.Client
}

func NewTelegram(token string, client *http.Client) *Telegram {
	return NewTelegramWithBase(apiBase, token, client)
}

func NewTelegramWithBase(base, token string, client *http.Client) *Telegram {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Telegram{base: base, token: token, http: client}
}

type photoSize struct {
	FileID string `json:"file_id"`
	Width  int    `json:"width"`
}

func (t *Telegram) Fetch(ctx context.Context, userID int64) ([]byte, error) {
	var photos struct {
		TotalCount int           `json:"total_count"`
		Photos     [][]photoSize `json:"photos"`
	}
	err := t.call(ctx, "getUserProfilePhotos", url.Values{
		"user_id": {strconv.FormatInt(userID, 10)},
		"limit":   {"1"},
	}, &photos)
	if err != nil {
		return nil, err
	}
	if len(photos.Photos) == 0 || len(photos.Photos[0]) == 0 {
		return nil, ErrNoPhoto
	}

	sizes := photos.Photos[0]
	best := sizes[0]
	for _, s := range sizes {
		if s.Width > best.Width {
			best = s
		}
	}

	var file struct {
		FilePath string `json:"file_path"`
	}
	if err := t.call(ctx, "getFile", url.Values{"file_id": {best.FileID}}, &file); err != nil {
		return nil, err
	}
	if file.FilePath == "" {
		return nil, ErrNoPhoto
	}
	return t.download(ctx, file.FilePath)
}

func (t *Telegram) call(ctx context.Context, method string, params url.Values, out any) error {
	endpoint := fmt.Sprintf("%s/bot%s/%s?%s", t.base, t.token, method, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var envelope struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if !envelope.OK {
		return fmt.Errorf("%s: telegram said %q", method, envelope.Description)
	}
	return json.Unmarshal(envelope.Result, out)
}

func (t *Telegram) download(ctx context.Context, filePath string) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/file/bot%s/%s", t.base, t.token, filePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download avatar: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || len(body) > maxBytes {
		return nil, fmt.Errorf("download avatar: got %d bytes", len(body))
	}
	return body, nil
}
