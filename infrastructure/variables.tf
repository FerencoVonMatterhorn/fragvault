variable "project_name" {
  description = "Short slug used to name/tag every resource."
  type        = string
  default     = "fragvault"
}

variable "location" {
  description = "Azure region for all resources."
  type        = string
  # swedencentral, chosen by what this subscription can actually deploy.
  # westeurope refuses new storage accounts ("region is currently not
  # accepting new customers") and northeurope offers no burstable VM SKU to
  # this subscription at all — only confidential-compute and GPU families,
  # at 6-10x the price. swedencentral has the full Bsv2/Basv2 range
  # unrestricted and prices ~10% under westeurope.
  default = "swedencentral"
}

variable "environment" {
  description = "Deployment environment tag (e.g. dev, staging, prod). Ends up in every resource name and tag."
  type        = string
  default     = "prod"
}

# --- Phase toggles --------------------------------------------------------
# Phase 1 only needs the hosting VM. The function app, GPU render VM, and
# blob storage are modeled now (per the full architecture) but left disabled
# by default so `terraform apply` today doesn't provision — and start
# billing for — pieces the app doesn't use yet, especially the GPU VM, which
# is the expensive one. Flip these on when each phase actually starts.

variable "enable_hosting_vm" {
  description = "Small Linux VM hosting the Phase 1 frontend+backend."
  type        = bool
  default     = true
}

variable "enable_blob_storage" {
  description = "Storage account + containers for retained demos and rendered highlight clips."
  type        = bool
  # On by default from Phase 3a: the backend uploads every parsed demo to the
  # `demos` container, and a match analysed without that is unrenderable
  # forever once Valve expires the URL. This costs cents per month and buys
  # back the entire back catalogue.
  default = true
}

variable "enable_function_app" {
  description = "Azure Function App for demo rendering (later phase)."
  type        = bool
  default     = false
}

variable "enable_gpu_render_vm" {
  description = "GPU VM that runs CS2 to render highlight clips (later phase, most expensive resource here)."
  type        = bool
  default     = false
}

# --- Hosting VM -------------------------------------------------------------

variable "hosting_vm_size" {
  description = "VM size for the small Linux box hosting frontend+backend."
  type        = string
  # Not Standard_B1s: the v1 B-series is unavailable to this subscription in
  # every mainstream region (SkuNotAvailable / NotAvailableForSubscription,
  # and it is not a quota problem — the BS family quota is 4 vCPUs, unused).
  # The v2 burstable family has no such restriction. B2ats_v2 is AMD, 2 vCPU
  # and 1 GB, and at ~EUR 0.0085/hr it is cheaper than a B1s was while giving
  # twice the cores. The ARM equivalent (B2pts_v2) saves another ~EUR 1/month
  # but needs an arm64 image and an arm64 Go build, which isn't worth it yet.
  default = "Standard_B2ats_v2"
}

variable "admin_username" {
  description = "Admin username for the hosting VM."
  type        = string
  default     = "fragvault"
}

variable "ssh_public_key" {
  description = "RSA SSH public key for admin access to the hosting VM. Must be RSA — Azure rejects ed25519 for Linux VM provisioning."
  type        = string

  # Caught here rather than by the provider, which fails mid-plan with
  # "the provided ssh-ed25519 SSH key is not supported" and no hint about
  # what to do instead. ed25519 is the better algorithm and works fine for
  # git and plain SSH — Azure's VM provisioning just doesn't accept it.
  validation {
    condition     = startswith(var.ssh_public_key, "ssh-rsa ")
    error_message = "Azure only accepts RSA keys for Linux VM provisioning. Generate one with: ssh-keygen -t rsa -b 4096 -C fragvault -f ~/.ssh/id_rsa_fragvault"
  }
}

variable "allowed_ssh_source_ip" {
  description = "CIDR allowed to reach the hosting VM on port 22 (lock this to your own IP, not 0.0.0.0/0)."
  type        = string
}

# --- GPU render VM (later phase) --------------------------------------------

variable "gpu_render_vm_size" {
  description = "VM size for the GPU box that runs CS2 to render highlight clips. Confirm quota AND availability in your region before enabling — a fresh subscription has zero N-series quota, and the SkuNotAvailable trap that bit the hosting VM applies to GPU families too."
  type        = string

  # NV4as_v4: 4 vCPU, 14 GiB RAM, 1/8th of a Radeon Instinct MI25 — a 2 GiB
  # frame buffer. That is enough for 1080p CS2 playback and not much more.
  # EUR 0.4476/hr Windows in swedencentral, the cheapest thing here that can
  # actually run the game.
  #
  # WARNING: Azure retires the entire NVv4 series on 2026-09-30. After that
  # date any surviving NVv4 VM is force-deallocated and loses SLA and support.
  # The migration is a change to this one value plus a re-run of the bootstrap
  # script — nothing else in this config knows the SKU. Targets, both verified
  # available in swedencentral:
  #   Standard_NV6ads_A10_v5  NVIDIA A10 1/6th, 4 GiB VRAM, GRID licence
  #                           included, NVENC. EUR 0.76/hr Windows.
  #   Standard_NG8ads_V620_v1 AMD V620 1/4th, 8 GiB VRAM. The family Microsoft
  #                           names for gaming workloads. EUR 1.05/hr Windows.
  # See docs/adr-003-render-vm.md.
  default = "Standard_NV4as_v4"
}
