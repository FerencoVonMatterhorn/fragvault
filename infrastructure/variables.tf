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
  description = "Storage account + container for rendered highlight clips."
  type        = bool
  default     = false
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
  description = "VM size for the GPU box that runs CS2 to render highlight clips. NCasT4_v3-series is the usual cost-effective single-GPU choice on Azure; confirm quota/availability in your region before enabling."
  type        = string
  default     = "Standard_NC4as_T4_v3"
}
