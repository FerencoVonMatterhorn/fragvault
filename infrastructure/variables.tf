# The contract: what can be set, and what the constraint behind each one is.
# The values themselves live in terraform.tfvars, which is committed — this is
# a single-environment repo and there is nothing secret in the answers.
#
# The two exceptions, which have no default and come from CI secrets, are
# marked below. Everything else is safe to read in public.

variable "project_name" {
  description = "Short slug used to name/tag every resource."
  type        = string
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
}

variable "environment" {
  description = "Deployment environment tag (e.g. dev, staging, prod). Ends up in every resource name and tag."
  type        = string
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
}

variable "admin_username" {
  description = "Admin username for the hosting VM and the GPU render VM."
  type        = string
}

variable "ssh_public_key" {
  description = "RSA SSH public key for admin access to the hosting VM. Must be RSA — Azure rejects ed25519 for Linux VM provisioning."
  type        = string

  # FROM CI SECRET: HOSTING_VM_SSH_PUBLIC_KEY.
  #
  # A public key is safe to commit, and this could move to terraform.tfvars —
  # but changing the value forces replacement of the hosting VM, taking the
  # Postgres volume and Caddy's certificates with it. Moving it is therefore a
  # copy-the-exact-current-value job, not a retype-it-from-memory job.

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
  description = "CIDR allowed to reach the hosting VM on port 22 and the GPU render VM on 3389. Lock this to your own address, not 0.0.0.0/0."
  type        = string

  # FROM CI SECRET: ADMIN_SOURCE_IP_CIDR.
  #
  # Deliberately not committed. It isn't a credential — reaching either box
  # still needs the SSH key or the RDP password — but publishing it on a public
  # repo advertises the one address worth attacking from, and reveals roughly
  # where the admin lives.
}

# --- GPU render VM ----------------------------------------------------------

variable "gpu_render_vm_size" {
  description = "VM size for the GPU box that runs CS2 to render highlight clips. Confirm quota AND availability in your region — a fresh subscription has zero N-series quota, and the SkuNotAvailable trap that bit the hosting VM applies to GPU families too."
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
}
