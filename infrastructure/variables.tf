variable "project_name" {
  description = "Short slug used to name/tag every resource."
  type        = string
  default     = "fragvault"
}

variable "location" {
  description = "Azure region for all resources."
  type        = string
  default     = "westeurope"
}

variable "environment" {
  description = "Deployment environment tag (e.g. poc, staging, prod)."
  type        = string
  default     = "poc"
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
  default     = "Standard_B1s"
}

variable "admin_username" {
  description = "Admin username for the hosting VM."
  type        = string
  default     = "fragvault"
}

variable "ssh_public_key" {
  description = "SSH public key for admin access to the hosting VM."
  type        = string
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
