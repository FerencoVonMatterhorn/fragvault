package demos

import (
	"context"
	"errors"
	"testing"
)

type stubDemoStore struct {
	name   string
	err    error
	called int
}

func (s *stubDemoStore) Retain(ctx context.Context, shareCode, path string) (string, error) {
	s.called++
	return s.name, s.err
}

func TestRetainDemoWithoutStore(t *testing.T) {
	w := &Worker{}

	if got := w.retainDemo(context.Background(), "CSGO-abc", "/tmp/demo.dem"); got != "" {
		t.Fatalf("expected no blob path without a store, got %q", got)
	}
}

func TestRetainDemoReturnsBlobName(t *testing.T) {
	store := &stubDemoStore{name: "CSGO-abc.dem"}
	w := &Worker{demoStore: store}

	got := w.retainDemo(context.Background(), "CSGO-abc", "/tmp/demo.dem")
	if got != "CSGO-abc.dem" {
		t.Fatalf("expected the blob name back, got %q", got)
	}
	if store.called != 1 {
		t.Fatalf("expected one upload, got %d", store.called)
	}
}

// The whole point of retention being best effort: a storage outage costs the
// ability to render that match later, and nothing else. The analysis has
// already spent minutes of CPU by this point.
func TestRetainDemoSwallowsFailure(t *testing.T) {
	w := &Worker{demoStore: &stubDemoStore{err: errors.New("container is on fire")}}

	if got := w.retainDemo(context.Background(), "CSGO-abc", "/tmp/demo.dem"); got != "" {
		t.Fatalf("expected no blob path after a failed upload, got %q", got)
	}
}
