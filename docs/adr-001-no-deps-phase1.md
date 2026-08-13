# ADR-001: Zero third-party Go dependencies for the Phase 1 backend

**Status:** accepted

## Context

The Phase 1 backend needs: HTTP routing, Steam OpenID verification, JSON handling, signed session cookies, and simple persistence. Common choices would pull in a web framework, a JWT library, and a SQL driver (e.g. for SQLite).

## Decision

Build the backend using only Go's standard library: `net/http`'s built-in method-pattern routing (Go 1.22+), `encoding/json`, `crypto/hmac` for session signing, and a mutex-guarded JSON file instead of SQLite for storage.

## Consequences

- The backend compiles and tests with `go build`/`go test` and no `go mod` fetches beyond what's already vendored by the Go toolchain itself — useful in restricted network environments, but also just fewer supply-chain dependencies for a service this small.
- No CGo dependency (which a SQLite driver would typically require), keeping cross-compilation and container builds simple.
- JSON-file storage does not scale past a handful of POC users and has no query capability beyond "look up by steamid" — expected to be replaced by real Azure-backed storage once the backend moves off the Phase 1 hosting VM. This is a deliberate, temporary trade-off, not an oversight.
- If a future phase needs something the standard library doesn't cover well (e.g. a Game Coordinator bot connection, or real demo parsing via `demoinfocs-golang`), add that dependency then — this ADR is about Phase 1's actual needs, not a permanent no-dependencies rule.
