<p align="center">
  <img src="assets/banner.svg" alt="FragVault — CS2 highlight discovery &amp; creation" width="900">
</p>

Sign in with Steam, and FragVault finds the moments worth rewatching in your CS2 matches — aces, clutches, opening picks and tight defuses — with a scoreboard and round history for each one.

**Live at [fragvault.pro](https://fragvault.pro).** Match discovery, demo analysis and highlight detection all work end to end. It runs as five containers on a single small Azure VM behind Caddy, which terminates TLS.

> Onboarding gotcha: the starting sharecode you paste is **exclusive**. Discovery walks *forward* from it, so pasting your most recent match's code finds nothing. Use an older one.

## How it runs

<p align="center">
  <img src="assets/architecture.svg" alt="Deployment topology: a browser reaches Caddy over HTTPS on one Azure VM; Caddy routes /api and /auth to the Go backend and everything else to the static frontend; the backend uses Postgres, asks a Node sidecar for demo URLs from the CS2 game coordinator, downloads demos from Valve, and calls the Steam Web API" width="1000">
</p>

A sharecode is not enough to fetch a demo: the download URL only comes from the **CS2 game coordinator**, over Steam's client protocol rather than any HTTP API, and no maintained Go library speaks it. That is the entire reason [`gc-sidecar/`](gc-sidecar/) exists — a deliberately thin Node service so everything else stays in Go.

Demos are downloaded, parsed, and **deleted immediately**; they run to hundreds of megabytes and the results are the part worth keeping. The events pulled out of them — kills, rounds, clutches, defuses — are stored, so improving a detector later re-derives highlights from the database rather than needing a demo Valve may already have expired.

## Roadmap

**Phase 1 — Match discovery** ✅ *live*
Sign in with Steam, onboard once, and have your recent matches discovered by walking sharecodes forward.

**Phase 2 — Analyse demos** ✅ *live*
The game coordinator resolves a sharecode to a demo URL, the demo is parsed, and highlights fall out: multi-kills, clutches, opening duels and defuses, each with a clip window. Every match gets a scoreboard (K/A/D, ADR, HS%, MVPs) and a round-by-round history. Parsing is separated from detection, so the rules that decide what counts as a clutch are unit-tested without a demo fixture.

**Phase 3 — Create highlights**
Turn those timestamps into actual video. A GPU VM runs CS2 against the demo and records the clip; a Function App orchestrates the job so nothing expensive stays running between renders; finished clips land in blob storage. All three already exist in Terraform behind `enable_gpu_render_vm`, `enable_function_app` and `enable_blob_storage`, disabled so they can't bill before the phase starts. The GPU VM is by far the most expensive resource in this repo — it should be created per job and destroyed after.

**Phase 4 — Share with friends**
Short links to rendered clips, served from blob storage through time-limited SAS URLs rather than a public container. Wants a clip page with an OpenGraph preview so a link dropped into Discord unfurls properly.

**Phase 5 — FACEIT connectivity**
Pull matches and demos from FACEIT alongside Valve matchmaking. FACEIT exposes both through its own API and hosts demos directly, so it sidesteps the game-auth-code onboarding entirely — and for a lot of players it's where the matches worth clipping actually happen.

### Also on the list

Smaller things, roughly in the order they'll start to hurt:

- **Encrypt the Valve auth codes at rest.** They sit in plaintext in a Postgres column. Fine for a single user; not fine for the first stranger who trusts the site with one.
- **Distinguish a rejected auth code from "no new matches".** The poller treats a 404 as "nothing newer", so bad credentials look exactly like an empty result.
- **Poll in the background** instead of on every `/api/matches` request, and stay clear of the Steam API rate limit as users are added.
- **Tighten the CI service principal.** It holds subscription-scope Contributor because Terraform creates the resource group itself; pre-creating the group and scoping to it would shrink the blast radius considerably.
- **Aggregate stats on the profile** — ADR and HS% across matches, most-played maps. The data is all there now.

## Layout

- `frontend/` — React + TypeScript + Vite
- `backend/` — Go HTTP API, demo parsing and highlight detection, Postgres persistence
- `gc-sidecar/` — thin Node service that resolves sharecodes to demo URLs via the CS2 game coordinator
- `function-app/` — placeholder for the later clip-rendering phase
- `infrastructure/` — Terraform for all infra (only the hosting VM is enabled by default; the rest sits behind `enable_*` variables so nothing bills before its phase starts)
- `deploy/` — compose file and Caddyfile that run the app on the VM
- `docs/` — architecture decision records

## Running locally

**Backend:**

```sh
cd backend
export DATABASE_URL=postgres://fragvault:fragvault@localhost:5432/fragvault?sslmode=disable
export BASE_URL=http://localhost:8080
export STEAM_WEB_API_KEY=your-steam-web-api-key   # https://steamcommunity.com/dev/apikey
export SESSION_SECRET=some-random-string
export DEV_INSECURE_COOKIE=true                    # allows the session cookie over plain http locally
go run ./cmd/server
```

Migrations run at boot, so an empty database is enough. `go test ./...` skips the database tests unless `TEST_DATABASE_URL` is set; CI provides one.

**Frontend:**

```sh
cd frontend
npm install
npm run dev
```

The Vite dev server proxies `/api` and `/auth` to `http://localhost:8080` (see `vite.config.ts`), so both need to be running together.

## Deploying

### The application

All three services are published as container images to ghcr.io on every push to `main`. **What's Up Docker** runs on the VM, watches those images and redeploys the app containers within five minutes, so a merge reaches production without an SSH session. Caddy and Postgres deliberately opt out — neither should restart unattended.

Changing `compose.yaml` or `.env` is still manual:

```sh
cd /opt/fragvault && docker compose pull && docker compose up -d
```

`/opt/fragvault` holds `compose.yaml` and `Caddyfile` (copies of the ones in `deploy/`) plus a `.env` that exists only on the VM — see [`deploy/.env.example`](deploy/.env.example) for its contents. Caddy obtains and renews the certificate for `fragvault.pro` by itself, which is why port 80 must stay open alongside 443.

Four volumes hold everything stateful:

| Volume | Contents |
|---|---|
| `postgres_data` | matches, scoreboards, rounds, events, highlights |
| `caddy_data` | certificates — `down -v` forces reissue, and Let's Encrypt's rate limits are unforgiving |
| `gc_data` | the sidecar's Steam refresh token |
| `fragvault_data` | the old JSON store, kept only for the one-time import |

Because the frontend and backend update independently, they are briefly out of step after every deploy. The client tolerates that by design — see the response normalisation in `frontend/src/api.ts`.

### The infrastructure

Terraform in `/infrastructure` provisions the Azure VM (and, later, the other infra pieces). Applied via GitHub Actions CI, not locally — see the workflow in `.github/workflows/`. A plan runs automatically on PRs and on merge to `main`; applying is a manual approval on the `production` environment in that same run.

**One-time state bootstrap.** Terraform state lives in an Azure Storage account that Terraform itself doesn't manage (it can't safely manage the thing holding its own state). Create it once with:

```sh
CI_CLIENT_ID=<the AZURE_CLIENT_ID repo secret> infrastructure/bootstrap/bootstrap-tfstate.sh
```

The script is idempotent and puts the account in its own resource group, so `terraform destroy` on the app resources can't take the state with it. The backend authenticates via Entra ID with the same service principal as the rest of CI, so there's no storage access key to store as a secret.

**SSH key.** The `HOSTING_VM_SSH_PUBLIC_KEY` secret must hold an **RSA** key — Azure rejects ed25519 for Linux VM provisioning, even though it's the better algorithm everywhere else:

```sh
ssh-keygen -t rsa -b 4096 -C fragvault -f ~/.ssh/id_rsa_fragvault
```

**Resource providers.** A fresh subscription has nothing registered beyond `Microsoft.Resources`, and the error you get is a misleading `(SubscriptionNotFound) Subscription <id> was not found`. The bootstrap script registers `Microsoft.Storage`; the VM needs two more:

```sh
az provider register --namespace Microsoft.Compute --wait
az provider register --namespace Microsoft.Network --wait
```
