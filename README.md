# FragVault

CS2 highlight discovery and creation. Phase 1 (this repo's current state): sign in with Steam, discover recent matches, list them.

## Layout

- `frontend/` — React + TypeScript + Vite POC UI, packaged behind nginx
- `backend/` — Go HTTP API (Steam auth + match polling)
- `docker-compose.yml` — runs both containers together; see "Running with Docker"
- `function-app/` — placeholder for the later demo-rendering phase
- `infrastructure/` — Terraform for all infra (only the hosting VM is enabled by default; the rest sits behind `enable_*` variables so nothing bills before its phase starts)

## Running locally

**Backend:**

```sh
cd backend
export BASE_URL=http://localhost:8080
export STEAM_WEB_API_KEY=your-steam-web-api-key   # https://steamcommunity.com/dev/apikey
export SESSION_SECRET=some-random-string
export DEV_INSECURE_COOKIE=true                    # allows the session cookie over plain http locally
go run ./cmd/server
```

**Frontend:**

```sh
cd frontend
npm install
npm run dev
```

The Vite dev server proxies `/api` and `/auth` to `http://localhost:8080` (see `vite.config.ts`), so both need to be running together.

## Running with Docker

Both services build to their own image (`backend/Dockerfile`, `frontend/Dockerfile`), and compose runs them the way the VM will: nginx in front serving the built frontend and proxying `/api` + `/auth` to the Go backend, which is never published to the host.

```sh
cp .env.example .env   # then fill in your Steam key and a session secret
docker compose up --build
```

Then open <http://localhost:8080>.

A few things worth knowing:

- **`BASE_URL` is the address the browser uses**, not the container's. Steam redirects the user back to it after login, so it has to match what you typed, port included.
- **Both containers run as non-root on a read-only root filesystem**, with all capabilities dropped. The backend gets a writable volume at `/data` for its JSON store; nginx gets a tmpfs at `/tmp` for its pid file and buffers. Nothing else is writable.
- **nginx listens on 8080, not 80** — a non-root process can't bind below 1024.
- **`BACKEND_ORIGIN` must have no trailing slash.** It goes into `proxy_pass` in a regex location, where a trailing slash would count as a URI part and strip the matched prefix.

CI builds both images on every push and pull request, and pushes them to GHCR on `main`:

```
ghcr.io/ferencovonmatterhorn/fragvault-backend:latest
ghcr.io/ferencovonmatterhorn/fragvault-frontend:latest
```

Both are also tagged with the full commit sha, which is what a deployment should pin to — `latest` only ever tracks `main` and is there for pulling by hand. Packages are private by default; make them public in the repo's package settings, or `docker login ghcr.io` before pulling.

## Deploying

Terraform in `/infrastructure` provisions the Azure VM (and, later, the other infra pieces). Applied via GitHub Actions CI, not locally — see the workflow in `.github/workflows/`.

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
