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
	// Where demos are downloaded to. They are deleted after parsing.
	tmpDir string
	// How long to wait after finding nothing to do.
	idleInterval time.Duration
}

func NewWorker(q Queue, tmpDir string) *Worker {
	return &Worker{queue: q, tmpDir: tmpDir, idleInterval: 5 * time.Second}
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

		if job == nil {
			if !w.sleep(ctx, w.idleInterval) {
				return
			}
			continue
		}

		w.process(ctx, *job)
	}
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

	return Result{
		MapName:    parsed.MapName,
		TickRate:   parsed.TickRate,
		Duration:   parsed.Duration,
		Highlights: Detect(parsed),
	}, nil
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
