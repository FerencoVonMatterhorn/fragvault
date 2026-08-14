package demos

import (
	"context"
	"log"
	"os"
	"time"
)

// Job is one queued analysis.
type Job struct {
	ID        int64
	ShareCode string
	DemoURL   string
}

// Result is what a finished analysis produced.
type Result struct {
	MapName    string
	TickRate   float64
	Duration   float64
	Highlights []Highlight
	Players    []PlayerStat
	Rounds     []Round
	// The events the highlights were derived from, stored so a later
	// detector change can re-derive without the demo.
	Kills      []Kill
	Clutches   []Clutch
	Defuses    []Defuse
	TeamAScore int
	TeamBScore int
}

// Rederive is an analysis whose events are stored but whose highlights were
// produced by an older detector version.
type Rederive struct {
	ID        int64
	ShareCode string
	Events    Parsed
}

// Avatars resolves steam ids to avatar URLs. Optional: a worker without one
// simply produces a scoreboard without pictures.
type Avatars interface {
	AvatarsFor(steamIDs []string) (map[string]string, error)
}

// Queue is the persistence the worker needs. Defined here, implemented by the
// db package, so the analysis code doesn't depend on the database and stays
// testable.
type Queue interface {
	// ClaimNext atomically takes the oldest pending job, or returns nil when
	// there is nothing to do.
	ClaimNext(ctx context.Context) (*Job, error)
	Complete(ctx context.Context, id int64, res Result) error
	Fail(ctx context.Context, id int64, reason string) error

	// ClaimNextRederive takes an analysis whose stored events outlive its
	// highlights, or nil when there is nothing to recompute.
	ClaimNextRederive(ctx context.Context) (*Rederive, error)
	SaveHighlights(ctx context.Context, id int64, highlights []Highlight) error
}

// Worker parses queued demos, strictly one at a time.
//
// The serialisation is deliberate and is the whole reason this is a worker
// rather than something done inline in a request. Parsing is 30-120 seconds
// of sustained CPU, and the VM is a burstable instance: running several at
// once spends its CPU credits and throttles the web server along with them.
// One job at a time keeps the site responsive while analysis is slow.
type Worker struct {
	queue Queue
	// Optional; nil means scoreboards without avatars.
	avatars Avatars
	// Where demos are downloaded to. They are deleted after parsing.
	tmpDir string
	// How long to wait after finding nothing to do.
	idleInterval time.Duration
}

func NewWorker(q Queue, avatars Avatars, tmpDir string) *Worker {
	return &Worker{queue: q, avatars: avatars, tmpDir: tmpDir, idleInterval: 5 * time.Second}
}

// Run processes jobs until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	log.Printf("demo analysis worker started (parser version %d)", ParserVersion)

	for {
		job, err := w.queue.ClaimNext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("error: claiming analysis job: %v", err)
			if !w.sleep(ctx, w.idleInterval) {
				return
			}
			continue
		}

		if job != nil {
			w.process(ctx, *job)
			continue
		}

		// Nothing to parse. Recomputing highlights from stored events is
		// seconds of CPU rather than minutes, and needs no demo, so it fills
		// the gaps rather than competing with real analysis.
		if did, err := w.rederive(ctx); err != nil {
			log.Printf("error: re-deriving highlights: %v", err)
		} else if did {
			continue
		}

		if !w.sleep(ctx, w.idleInterval) {
			return
		}
	}
}

// rederive recomputes one analysis's highlights from its stored events.
// Reports whether there was anything to do.
func (w *Worker) rederive(ctx context.Context) (bool, error) {
	job, err := w.queue.ClaimNextRederive(ctx)
	if err != nil || job == nil {
		return false, err
	}

	highlights := Detect(job.Events)
	if err := w.queue.SaveHighlights(context.WithoutCancel(ctx), job.ID, highlights); err != nil {
		return true, err
	}
	log.Printf("re-derived %s from stored events: %d highlights (detector v%d)",
		job.ShareCode, len(highlights), DetectorVersion)
	return true, nil
}

func (w *Worker) process(ctx context.Context, job Job) {
	started := time.Now()
	log.Printf("analysing %s (job %d)", job.ShareCode, job.ID)

	res, err := w.analyse(ctx, job)
	if err != nil {
		// A failure is recorded, not retried. An expired demo or a bad URL
		// will fail identically forever, and a retry loop against Valve is a
		// good way to get rate-limited.
		log.Printf("analysis of %s failed after %s: %v", job.ShareCode, time.Since(started).Round(time.Second), err)
		if ferr := w.queue.Fail(context.WithoutCancel(ctx), job.ID, err.Error()); ferr != nil {
			log.Printf("error: recording failure for job %d: %v", job.ID, ferr)
		}
		return
	}

	if err := w.queue.Complete(context.WithoutCancel(ctx), job.ID, res); err != nil {
		log.Printf("error: recording completion for job %d: %v", job.ID, err)
		return
	}
	log.Printf("analysed %s in %s: %d highlights on %s",
		job.ShareCode, time.Since(started).Round(time.Second), len(res.Highlights), res.MapName)
}

func (w *Worker) analyse(ctx context.Context, job Job) (Result, error) {
	path, err := Download(ctx, job.DemoURL, w.tmpDir)
	if err != nil {
		return Result{}, err
	}
	// Demos are hundreds of MB and the disk is small: the file goes away as
	// soon as it has been read, whatever happens next.
	defer os.Remove(path)

	parsed, err := ParseFile(path)
	if err != nil {
		return Result{}, err
	}

	w.addAvatars(parsed.Players)

	return Result{
		MapName:    parsed.MapName,
		TickRate:   parsed.TickRate,
		Duration:   parsed.Duration,
		Highlights: Detect(parsed),
		Players:    parsed.Players,
		Rounds:     parsed.Rounds,
		Kills:      parsed.Kills,
		Clutches:   parsed.Clutches,
		Defuses:    parsed.Defuses,
		TeamAScore: parsed.TeamAScore,
		TeamBScore: parsed.TeamBScore,
	}, nil
}

// addAvatars fills in avatar URLs in place.
//
// Best effort on purpose: a missing avatar is a cosmetic gap, and failing an
// analysis that already cost minutes of CPU because Steam's profile API had a
// bad minute would be a poor trade.
func (w *Worker) addAvatars(players []PlayerStat) {
	if w.avatars == nil || len(players) == 0 {
		return
	}

	ids := make([]string, 0, len(players))
	for _, p := range players {
		ids = append(ids, p.SteamID)
	}

	found, err := w.avatars.AvatarsFor(ids)
	if err != nil {
		log.Printf("warning: could not fetch avatars: %v", err)
		// Whatever came back before the error is still worth using.
	}
	for i := range players {
		if url, ok := found[players[i].SteamID]; ok {
			players[i].AvatarURL = url
		}
	}
}

// sleep reports false if the context was cancelled while waiting.
func (w *Worker) sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
