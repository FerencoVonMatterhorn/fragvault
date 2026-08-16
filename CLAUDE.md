# Working on FragVault

Things that aren't obvious from reading the code, and that have already cost a
debugging round each. Everything here is public — this repo is public, so no
secrets in this file or any other.

## Azure: what this subscription can actually deploy

- **Don't "optimise" the VM to `Standard_B1s`.** It looks cheaper on paper and
  is unavailable to this subscription in every mainstream region —
  `SkuNotAvailable` at apply time, `NotAvailableForSubscription` in the SKU
  API. It is *not* a quota problem; the BS family quota is 4 vCPUs and unused.
  The v1 B-series only exists in DenmarkEast and IndiaSouthCentral.
- The **v2 burstable families (Bsv2 / Basv2) are unrestricted.**
  `Standard_B2ats_v2` is what's deployed: 2 vCPU, cheaper than a B1s.
- **Region is `swedencentral` deliberately.** `northeurope` offers this
  subscription no burstable SKU at all — only confidential-compute and GPU
  families at 6-10x the price. `westeurope` refuses new storage accounts
  ("region is currently not accepting new customers").
- A fresh subscription registers no resource providers, and the resulting
  error is a misleading `(SubscriptionNotFound)`. See the README.

## Terraform

- **State lives outside Terraform on purpose.** The storage account is created
  by `infrastructure/bootstrap/bootstrap-tfstate.sh`, in its own resource group
  in `northeurope`. Don't import it or move it into the config — a state
  account managed by its own state is destroyable by the run that needs it.
- Auth to that backend is Entra ID (`use_azuread_auth`), not an access key.
  There is no storage key secret, and shared key access is disabled.
- **Applies are gated.** `terraform.yml` plans automatically and the apply job
  waits on the `production` environment's required reviewers, in the same run.
  Don't add auto-apply on merge.
- CI and local Terraform are both pinned to 1.15.8. Keep them equal, or state
  written locally becomes unreadable to CI.

## Application

- **Caddy owns routing** (`deploy/Caddyfile`): `/api` and `/auth` go to the
  backend, everything else to the frontend. The frontend's nginx is
  static-only. Don't re-add a proxy there — it was removed precisely because
  two definitions of the same routing drift apart.
- `/api` and `/auth` must stay same-origin with the app: the frontend sends
  cookies with `credentials: "include"`, and Steam OpenID redirects to
  `BASE_URL`.
- **Third-party Go dependencies are fine.** The old stdlib-only rule came
  from a build sandbox that couldn't reach package registries; that
  constraint is gone. `pgx` and `demoinfocs-golang` are in use. Still prefer
  the stdlib where it's genuinely enough — auth, sessions and HTTP handling
  have no reason to grow dependencies.
- **Persistence is PostgreSQL**, in a container, with migrations applied at
  boot from `backend/internal/db/migrations` by a small embedded runner.
  See `docs/adr-002-database.md`. The JSON-file store is gone; its importer
  runs once and refuses if the database already has players.
- Re-analysis is prevented by `UNIQUE (share_code, parser_version)` on
  `demo_analyses`. A failed analysis can be retried; finished ones can't.
- **Two versions, and the difference matters.** `ParserVersion` is how events
  were pulled out of the demo — bumping it needs the demo again, which Valve
  may have expired. `DetectorVersion` is how highlights were derived from
  those events — bumping it re-derives from the database, needs no demo, and
  works on matches whose demos are long gone. **Bump the detector version
  unless the extracted events themselves change.**
- Highlights exist for *every* player in a demo, not just the account that
  owns the match. Anything user-facing has to filter by steam id, or it will
  cheerfully show someone an enemy's ace as their own.
- **Demo URLs only come from the CS2 game coordinator.** There is no Steam
  Web API for them, and no maintained Go library speaks that protocol — hence
  `gc-sidecar/`, a thin Node service. Don't go looking for an HTTP endpoint
  that returns a demo URL; it doesn't exist.
- The sidecar needs a **dedicated, disposable Steam account**. Its dependency
  tree carries CVEs it can't escape (`steam-user` → `protobufjs`, `adm-zip`),
  documented in `gc-sidecar/README.md`.

## The GPU render VM (`docs/adr-003-render-vm.md`)

- **The render VM needs its own second Steam account.** The sidecar holds
  `gamesPlayed([730])` for as long as it's connected and Steam allows one game
  session per account, so running CS2 under the sidecar's account kicks it off
  the game coordinator. That surfaces as "no new matches" — indistinguishable
  from there genuinely being none.
- **`Standard_NV4as_v4` retires 2026-09-30.** After that Azure force-
  deallocates it. The SKU lives in exactly one place (`gpu_render_vm_size`);
  the migration targets are in the comment there.
- **Always launch CS2 with `-insecure`.** HLAE injects a DLL and VAC will
  eventually notice. This is the other reason the account is disposable.
- **Creating the VM needs more than Contributor.** The CI service principal
  gets subscription-scope Contributor from `bootstrap-tfstate.sh`, which cannot
  create role assignments. `enable_gpu_render_vm` also needs *Role Based Access
  Control Administrator* on it, or the apply fails with `AuthorizationFailed`.
- **`azurerm_windows_virtual_machine` cannot create from the golden image.** It
  always emits an `osProfile`, and Azure rejects that for *specialized* images
  — which is the only kind that survives Steam's machine-bound login. Hence
  `scripts/capture-golden-image.sh` and a documented `az vm create` + `terraform
  import` restore path. Don't "fix" this by generalizing the image.
- The bootstrap script is embedded into the extension's settings by `file()`,
  so its bytes are part of the plan — `.ps1` is pinned to LF in `.gitattributes`
  or Windows and CI hash differently and re-run the bootstrap every apply.
- **Demos are now retained** to the `demos` blob container before the local
  copy is deleted, recorded in `demo_analyses.demo_blob_path`. Upload is best
  effort and never fails an analysis, so an empty path is an ordinary value
  meaning "not renderable". Blobs may still be bzip2 — sniff the magic bytes
  like `ParseFile` does, don't trust the `.dem` suffix.
- Analysis parsing is separated from detection on purpose: `Parse` emits
  plain events, detectors are pure functions over them, and that's what makes
  them testable without a huge demo fixture. Keep it that way.
- Secrets live in `/opt/fragvault/.env` **on the VM only**. `deploy/.env.example`
  is the committed template.
- Deployment is manual: `docker compose pull && docker compose up -d`.
- `docs/` exists again, but **only for ADRs**. Everything else durable goes
  here or in the README.

## Behaviour worth knowing

- **The onboarding sharecode is exclusive.** Discovery walks *forward* from it,
  so the most recent match's code finds nothing and looks like a broken
  integration.
- `poller.go` treats a 404 as "no newer match", so a rejected auth code is
  indistinguishable from an empty result. Known rough edge.
- Onboarded users' Valve auth codes are stored **in plaintext** in a JSON file.
  Fine for the current single user; fix before anyone else uses this.

## This machine (Windows)

- **Always show command output.** Don't use `--output none`, `2>/dev/null`, or
  `>$null`, and never swallow an error into a fallback message — a hidden
  failure here once reported a successful Azure role assignment that hadn't
  happened.
- `git commit -m` with a PowerShell here-string mangles multi-line messages
  into bogus pathspecs. Write the message to a file and use `git commit -F`.
- Git Bash rewrites arguments that look like paths, so an ARM scope like
  `/subscriptions/...` becomes `C:/Program Files/Git/subscriptions/...` and
  Azure reports `MissingSubscription`. `MSYS2_ARG_CONV_EXCL` opts out.
