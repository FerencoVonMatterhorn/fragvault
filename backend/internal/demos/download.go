package demos

import (
	"bufio"
	"compress/bzip2"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// maxDemoBytes caps what a single download may write to disk. Valve demos run
// to a few hundred MB; the OS disk is 30 GB and shared with everything else,
// so an unbounded io.Copy from a URL a user supplied is not acceptable.
const maxDemoBytes = 800 << 20 // 800 MiB

// downloadTimeout is generous because these are large files over a link we
// don't control, but bounded because a stuck download would otherwise hold
// the single analysis worker forever.
const downloadTimeout = 15 * time.Minute

// Download fetches a demo into dir and returns the path. The caller owns the
// file and must remove it — demos are temporary by design, since the analysis
// results are the thing worth keeping.
func Download(ctx context.Context, url, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching demo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Valve expires matchmaking demos, so a 404 here is an ordinary
		// outcome rather than a bug — worth saying so plainly in the message
		// that ends up in demo_analyses.error.
		if resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("demo not found (404) — Valve keeps matchmaking demos for a limited time, so it may have expired")
		}
		return "", fmt.Errorf("fetching demo: unexpected status %d", resp.StatusCode)
	}

	f, err := os.CreateTemp(dir, "demo-*.dem")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	path := f.Name()

	written, err := io.Copy(f, io.LimitReader(resp.Body, maxDemoBytes+1))
	closeErr := f.Close()
	if err != nil {
		os.Remove(path)
		return "", fmt.Errorf("writing demo to disk: %w", err)
	}
	if closeErr != nil {
		os.Remove(path)
		return "", fmt.Errorf("closing demo file: %w", closeErr)
	}
	if written > maxDemoBytes {
		os.Remove(path)
		return "", fmt.Errorf("demo exceeds the %d MiB limit", maxDemoBytes>>20)
	}

	return path, nil
}

// ParseFile parses a demo from disk, transparently handling the bzip2
// compression Valve serves demos with.
//
// Detection is by magic bytes rather than file extension: the URL a demo
// arrives from may be redirected, signed, or simply named badly.
func ParseFile(path string) (Parsed, error) {
	f, err := os.Open(path)
	if err != nil {
		return Parsed{}, fmt.Errorf("opening demo: %w", err)
	}
	defer f.Close()

	// Buffered so the magic bytes can be inspected without consuming them.
	br := bufio.NewReader(f)
	magic, err := br.Peek(3)
	if err != nil {
		return Parsed{}, fmt.Errorf("reading demo header: %w", err)
	}

	var r io.Reader = br
	if string(magic) == "BZh" {
		r = bzip2.NewReader(br)
	}

	return Parse(r)
}
