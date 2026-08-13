# FragVault — Architecture (Phase 1)

## What this phase proves

Can we authenticate a CS2 player via Steam and reliably discover their recent matches? Everything else (rendering, highlight editing, FACEIT) depends on this working.

## Steam login

Steam has no OAuth — authentication is OpenID 2.0. `internal/steamauth` implements the redirect (`/auth/steam/login`) and the callback verification (`/auth/steam/callback`), using OpenID's "stateless" direct verification (posting the assertion back to Steam and checking for `is_valid:true`) rather than tracking association handles, since we don't need OpenID's replay-protection state for a login-only use case. After verifying identity we call `GetPlayerSummaries` for a display name and avatar.

## Match discovery — and why it's not a simple API call

Valve does not expose a "list this player's matches" endpoint. The only officially supported mechanism (documented on Valve's developer wiki) is:

1. The player gets a one-time **game authentication code** from `help.steampowered.com`, plus a **starting sharecode** from CS2's in-game match history settings, and provides both to us once during onboarding.
2. We poll `GetNextMatchSharingCode` (`ICSGOPlayers_730` Steam Web API) with `steamid` + `steamidkey` (the auth code) + `knowncode`, walking forward one sharecode at a time until the API reports there's nothing newer.
3. Each sharecode is a base57-encoded 18-byte value containing `matchId`, `reservationId` (sometimes called `outcomeId`), and `tvPort`. `internal/matches/sharecode.go` decodes this — the format isn't Valve-documented, so our implementation is cross-checked against the community reference `akiver/csgo-sharecode` and covered by a round-trip test, but hasn't yet been validated against a sharecode from a real account.

**This is deliberately the same pipeline the later demo-rendering phase needs** — the decoded matchId/reservationId/tvPort are what let you construct a demo download URL from Valve's replay CDN. That URL pattern is community reverse-engineered too, and is a good first thing to validate for real once this phase is live.

### Known limitation

A sharecode alone gives you an identifier and discovery order — not map, score, or K/D. Getting those without a Steam Game Coordinator bot account (a persistent, semi-fragile connection Valve doesn't provide an official client library for in Go) means downloading and reading the demo file. `markus-wa/demoinfocs-golang` is a mature, actively maintained Go library for this, and reading just the demo header (not the full replay) is cheap — a natural near-term enhancement once this phase is solid, but out of scope for Phase 1 by design.

## Why the backend has zero third-party Go dependencies

This was originally a workaround for the build sandbox's package-registry restrictions, but it's a reasonable choice on its own merits for a service this size: Go's standard library covers HTTP routing (1.22+'s method-pattern `ServeMux`), JSON, and HMAC signing without reaching for a framework, session library, or ORM. Storage is a mutex-guarded JSON file (`internal/matches/store.go`) rather than SQLite specifically to avoid needing a CGo/driver dependency — swap this for real Azure-backed storage when the backend moves off the Phase 1 VM.

## Deployment topology

One small Azure Linux VM runs nginx as the front door: it reverse-proxies `/api/*` and `/auth/*` to the Go backend (a systemd service) and serves the Vite production build as static files for everything else. See `/infrastructure` for the Terraform and `/docs/adr-001-no-deps-phase1.md`.

## Infra-as-code scope

`/infrastructure` models the full target architecture — hosting VM, blob storage for rendered clips, an Azure Function App for demo rendering, and a GPU VM (running the CS2 golden image) for the actual rendering workload — but only the hosting VM is enabled by default (`enable_hosting_vm = true`, everything else `false`). The other three are real, valid Terraform, just gated behind variables so `terraform apply` today doesn't start provisioning — and billing for — infrastructure nothing uses yet. Flip each on when its phase actually starts.

`terraform apply` is intentionally never run from the AI build sandbox that produced this code (see below) — it runs via GitHub Actions CI using an Azure service principal.

## Terraform state

State lives in an Azure Storage account (`stfragvaulttfstate`, container `tfstate`) in its own resource group, created by `infrastructure/bootstrap/bootstrap-tfstate.sh` rather than by Terraform. Two reasons for keeping it outside: a state account managed by its own state is destroyable by the run that depends on it, and the separate resource group means a `terraform destroy` of the app infrastructure leaves state intact.

Configuration choices there are all cost-driven — Standard LRS (no geo-replication), hot tier, no private endpoint (that alone would cost more per month than everything else in Phase 1 combined). The state file is tens of KB, so the account runs at cents per month. Blob versioning and 7-day soft delete are on because they're free at that size; a lifecycle rule prunes versions after 30 days so they can't accumulate. Locking uses the backend's built-in blob lease — no separate lock resource to pay for.

Auth is Entra ID (`use_azuread_auth`), not a storage access key, so the only credentials CI holds are the service principal's. Shared key access is disabled on the account; anyone running Terraform locally needs the "Storage Blob Data Contributor" role on it.

## A note on how this was built

This codebase was scaffolded inside a network-sandboxed cloud AI environment that, at various points, could not reach npm, PyPI, Azure's management API, or GitHub's API depending on evolving account/org network settings. That's why: the backend avoids third-party dependencies (buildable/testable even under those restrictions), the frontend and Terraform were validated where possible (`terraform validate` against the real provider; the frontend could not run `npm install` in that environment and should be verified in CI or locally before being trusted), and infra apply + repo push route through GitHub Actions / a manually-created repo rather than being run directly by the assistant. Worth knowing if something here looks like it was designed around an odd constraint — it was.
