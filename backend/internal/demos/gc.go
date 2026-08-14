package demos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ErrNoDemoAvailable means the game coordinator answered but had no demo —
// almost always because Valve has expired it. Distinguished from a transport
// failure so the user gets "it expired" rather than "something broke".
var ErrNoDemoAvailable = errors.New("no demo available for this match")

// GCResolver turns a sharecode into a demo URL by asking the sidecar.
//
// The sidecar exists because the demo URL only comes from the CS2 game
// coordinator, which is a Steam client protocol conversation rather than an
// HTTP API, and no maintained Go library speaks it. See gc-sidecar/README.md.
type GCResolver struct {
	baseURL string
	client  *http.Client
}

func NewGCResolver(baseURL string) *GCResolver {
	return &GCResolver{
		baseURL: baseURL,
		// Generous: the sidecar serialises requests and waits on the GC, so a
		// queued request can legitimately sit for a while.
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Resolve returns the demo URL for a sharecode.
func (r *GCResolver) Resolve(ctx context.Context, shareCode string) (string, error) {
	endpoint := fmt.Sprintf("%s/demo-url?sharecode=%s", r.baseURL, url.QueryEscape(shareCode))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("building sidecar request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("asking the game coordinator sidecar: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		DemoURL string `json:"demo_url"`
		Error   string `json:"error"`
	}
	// Decoding errors are tolerated here: the status code carries the meaning
	// and a malformed body shouldn't mask it.
	_ = json.NewDecoder(resp.Body).Decode(&body)

	switch resp.StatusCode {
	case http.StatusOK:
		if body.DemoURL == "" {
			return "", ErrNoDemoAvailable
		}
		return body.DemoURL, nil
	case http.StatusNotFound:
		if body.Error != "" {
			return "", fmt.Errorf("%w: %s", ErrNoDemoAvailable, body.Error)
		}
		return "", ErrNoDemoAvailable
	case http.StatusServiceUnavailable:
		// Not connected to the GC. Transient — worth retrying later, unlike
		// an expired demo.
		return "", fmt.Errorf("game coordinator unavailable: %s", body.Error)
	default:
		return "", fmt.Errorf("sidecar returned %d: %s", resp.StatusCode, body.Error)
	}
}
