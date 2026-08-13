# FragVault

CS2 highlight discovery and creation. Phase 1 (this repo's current state): sign in with Steam, discover recent matches, list them.

See [`docs/architecture.md`](docs/architecture.md) for the design and its known limitations, and [`docs/adr-001-no-deps-phase1.md`](docs/adr-001-no-deps-phase1.md) for why the backend has no third-party dependencies.

## Layout

- `frontend/` — React + TypeScript + Vite POC UI
- `backend/` — Go HTTP API (Steam auth + match polling)
- `function-app/` — placeholder for the later demo-rendering phase
- `infrastructure/` — Terraform for all infra (only the hosting VM is enabled by default — see `docs/architecture.md`)
- `docs/` — architecture notes and ADRs

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
