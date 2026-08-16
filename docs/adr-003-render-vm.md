# ADR-003: A long-lived GPU VM that renders clips with HLAE

**Status:** accepted
**Date:** 2026-08-16
**Amends:** the Phase 3 sketch in `README.md` — a Function App orchestrating a VM created per job

## Context

Phase 2 produces highlights with exact tick ranges and gets rid of the demo within seconds of parsing it. Turning a tick range into video needs three things the project doesn't have: a machine that can run CS2, a way to record CS2 playback unattended, and the demo file at render time rather than analysis time.

CS2 has no headless renderer and no server-side movie mode. There is no way to produce a clip without running the actual game against the actual demo on an actual GPU. That constraint drives every decision below.

## Decision

### `Standard_NV4as_v4`, knowing it retires on 2026-09-30

4 vCPU, 14 GiB RAM, and 1/8th of a Radeon Instinct MI25 — a 2 GiB frame buffer. At **EUR 0.4476/hr** (Windows, swedencentral) it is the cheapest SKU in this subscription's reach that can run the game at all, roughly 40% under the alternatives.

Azure retires the whole NVv4 series on **30 September 2026**, after which surviving VMs are force-deallocated and lose SLA and support. That is weeks, not years, and it is accepted deliberately: the first question is whether a cloud VM can render a watchable CS2 clip unattended at all, and this is the cheapest way to find out. Everything is built so the migration is a one-line change:

- The SKU appears exactly once, in `gpu_render_vm_size`.
- The machine is defined by `scripts/gpu-render-bootstrap.ps1`, not assembled by hand.
- Nothing else in the config knows the GPU vendor.

The two targets, both confirmed available in `swedencentral`:

| SKU | GPU | VRAM | EUR/hr (Win) | Note |
|---|---|---|---|---|
| `Standard_NV6ads_A10_v5` | NVIDIA A10 ⅙ | 4 GiB | 0.76 | GRID licence included, NVENC available |
| `Standard_NG8ads_V620_v1` | AMD V620 ¼ | 8 GiB | 1.05 | the family Microsoft names for gaming workloads |

If 2 GiB of frame buffer turns out to be the thing standing between us and a watchable clip, that is a useful finding and the migration was coming anyway.

### HLAE piping to ffmpeg, not OBS

HLAE's `mirv_streams` renders the demo at a fixed `host_framerate` and pipes frames straight into ffmpeg, with audio via `startMovieWav`. Because the render is deterministic rather than realtime, output quality does not depend on the VM sustaining a frame rate — a slow GPU produces the same file, more slowly. On a 2 GiB partition of a shared MI25 that property is worth a great deal.

OBS would add a second recorder, a websocket to script, and a hard dependency on realtime performance, in exchange for nothing this pipeline needs. It is not installed.

HLAE injects a DLL, so **CS2 is always launched with `-insecure`**, and the account it runs under must be one nobody minds losing.

### One VM, started and deallocated — not created per job

The README originally called for creating the VM per job and destroying it after. Start/deallocate is better on every axis that matters here:

- A deallocated VM bills no compute. The idle cost is the 128 GB Premium OS disk, ~EUR 17/month.
- Starting takes 60-90 seconds. Provisioning a 128 GB Windows box and installing 35 GB of CS2 takes closer to 45 minutes, and the game files cannot be baked into a marketplace image.
- Terraform stays out of the runtime path. The orchestrator only ever calls start and deallocate, so creating infrastructure remains something that happens in a reviewed, approval-gated apply.

An `azurerm_dev_test_global_vm_shutdown_schedule` at 03:00 is the backstop, not the mechanism — left running, this VM costs ~EUR 320/month.

### A separate, disposable Steam account

`gc-sidecar/index.js` holds `client.gamesPlayed([730])` for as long as it is connected, and Steam permits one game session per account. Running CS2 on the render VM under the sidecar's account kicks the sidecar off the game coordinator and silently breaks match discovery — which surfaces as "no new matches", indistinguishable from there genuinely being none.

So: a second dedicated account, no Prime needed (CS2 is free-to-play), treated as disposable for the same reasons the sidecar's is.

### The golden image is specialized, and lives outside Terraform

The Steam login is machine-bound. Sysprep resets the machine SID, which invalidates the sentry file and forces a fresh Steam Guard challenge on every rebuild — so a generalized image would defeat the point of capturing one.

`scripts/capture-golden-image.sh` therefore captures a **specialized** image into an Azure Compute Gallery, no sysprep, retaining the logged-in Steam session and the 35 GB of game files. The cost is that `azurerm_windows_virtual_machine` cannot create from it: the provider always emits an `osProfile`, and Azure rejects that for specialized sources. Restoring is an `az vm create --specialized` followed by `terraform import`, documented at the bottom of that script.

That is an acceptable trade because the image is a **restore artifact, not a definition**. The definition is `gpu-render-bootstrap.ps1` plus the manual steps it prints, both of which are in git. This is the same split as `bootstrap/bootstrap-tfstate.sh`: things Terraform structurally cannot own live in an idempotent script beside it.

### Demos are retained in blob storage

`demo_analyses.demo_url` points at a Valve URL that expires after a few weeks, and the local file is deleted the moment parsing ends. Without a second copy, only matches analysed in the last few weeks could ever be rendered — exactly the failure the stored-events tables were introduced to avoid.

The analysis worker now uploads each demo it parses to a `demos` container and records the name in `demo_analyses.demo_blob_path` (migration `0007`). Authentication is a container-scoped SAS in `.env`, matching every other credential on the box; a managed identity on the hosting VM would be cleaner and is a follow-up.

Upload is **best effort and never fails an analysis**. The parse has already cost minutes of CPU by then, and losing that because storage had a bad minute would be a poor trade. An empty `demo_blob_path` is an ordinary value meaning "not renderable", and rendering has to handle it.

Neither `ParserVersion` nor `DetectorVersion` is bumped: nothing about how events are extracted or how highlights are derived has changed.

## Consequences

- **A hard deadline in six weeks.** By 2026-09-30 the SKU must change or the VM stops. The variable comment carries the date.
- **Role assignments need more than Contributor.** The CI service principal holds subscription-scope Contributor from `bootstrap-tfstate.sh`, which cannot create role assignments. It also needs *Role Based Access Control Administrator* (or Owner) before `enable_gpu_render_vm` can apply, or the two `azurerm_role_assignment` resources fail with `AuthorizationFailed`.
- **The Steam login is a manual step, once.** Automating Steam Guard is more fragile than clearing it by hand and capturing the result. The bootstrap script prints what remains to be done.
- **The render VM is a pet, not cattle.** A specialized image and a machine-bound login mean this box has an identity. Acceptable for one appliance; it would not be for a fleet, and if renders ever need to run in parallel this decision is the first one to revisit.
- **Demos now cost storage.** Capped at 800 MiB each by the downloader, tiered to Cool after seven days. Archive is deliberately avoided — rehydration takes hours and would turn a render into an overnight job.
- **`function_app.tf` is now dead weight.** Orchestration will live in the Go backend beside the existing analysis worker, which already has the queue, the claim pattern and the database connection. The file stays inert until that lands, then goes.

## Alternatives rejected

**Linux + Proton or the native CS2 Linux build.** Roughly 40% cheaper per hour. HLAE is a Windows DLL injector with no Linux equivalent, so this means capturing an X session with ffmpeg in realtime — losing deterministic rendering, camera control and any hope of frame-perfect output. The saving is not worth being unable to control the camera.

**A Function App orchestrating per-job VM creation.** The original sketch. Rejected with the lifecycle decision above: it puts resource creation on the hot path, pays 45 minutes of provisioning per job, and adds a runtime whose only job is to call two ARM endpoints the backend can call itself.

**Generalized golden image.** Would let Terraform own the VM's source image end to end. Rejected because it breaks the Steam login on every rebuild, which is the one thing the image exists to preserve.

**Re-resolving the demo URL from the game coordinator at render time.** No storage cost and no worker change. Rejected because it fails for any match past Valve's retention window, making the back catalogue permanently unrenderable — and it would compete with match discovery for the sidecar's single serialised request channel.
