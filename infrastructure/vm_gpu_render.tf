# GPU VM that runs a real CS2 client against a demo file and records highlight
# clips with HLAE. Disabled by default (enable_gpu_render_vm = false) — at
# EUR 0.4476/hr this is by far the most expensive resource in the config, and
# leaving it running is ~EUR 320/month. Turning it on is a deliberate act; see
# the workflow variable in terraform.yml.
#
# Lifecycle: Terraform owns exactly one VM. It is not created and destroyed per
# job — it is started and deallocated. A deallocated VM bills nothing for
# compute, only the OS disk (~EUR 17/month), and starting takes 60-90s against
# 5-8 minutes to provision a 128 GB Windows box from scratch. That also keeps
# Terraform out of the runtime path: the orchestrator only ever calls
# start/deallocate, never apply.
#
# The machine is defined by scripts/gpu-render-bootstrap.ps1, exactly as the
# hosting VM is defined by cloud-init/hosting.yaml.tftpl. Editing that script
# re-runs it. See docs/adr-003-render-vm.md.

resource "azurerm_public_ip" "gpu_render" {
  count               = var.enable_gpu_render_vm ? 1 : 0
  name                = "pip-${local.name_prefix}-gpu-render"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  allocation_method   = "Static"
  sku                 = "Standard"
  tags                = local.common_tags
}

resource "azurerm_network_security_group" "gpu_render" {
  count               = var.enable_gpu_render_vm ? 1 : 0
  name                = "nsg-${local.name_prefix}-gpu-render"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = local.common_tags

  # RDP only, and only from the admin IP — this box runs a real Windows
  # desktop/game session, not a public web service. RDP is a maintenance door:
  # rendering happens in the auto-logon console session, and connecting over
  # RDP while a render is running will disturb it.
  #
  # Reuses allowed_ssh_source_ip rather than adding a near-identical variable;
  # both mean "the CIDR the admin administers from".
  security_rule {
    name                       = "AllowRDPFromAdmin"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "3389"
    source_address_prefix      = var.allowed_ssh_source_ip
    destination_address_prefix = "*"
  }
}

resource "azurerm_network_interface" "gpu_render" {
  count               = var.enable_gpu_render_vm ? 1 : 0
  name                = "nic-${local.name_prefix}-gpu-render"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = local.common_tags

  ip_configuration {
    name = "internal"
    # Reuses the hosting VNet/subnet, so this VM cannot exist without the
    # hosting VM. That is fine — the backend that will drive it lives there,
    # and a second VNet buys nothing while both boxes are in one region.
    subnet_id                     = azurerm_subnet.hosting[0].id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.gpu_render[0].id
  }
}

resource "azurerm_network_interface_security_group_association" "gpu_render" {
  count                     = var.enable_gpu_render_vm ? 1 : 0
  network_interface_id      = azurerm_network_interface.gpu_render[0].id
  network_security_group_id = azurerm_network_security_group.gpu_render[0].id
}

variable "gpu_render_admin_password" {
  description = "Local admin password for the GPU render VM. Windows VMs require one. Pass via CI secret / a tfvars file that's gitignored — never commit a real value. Left with no default on purpose so `terraform apply` fails loudly instead of silently applying a placeholder password."
  type        = string
  sensitive   = true
  default     = null

  # An unset GitHub secret interpolates to "" rather than disappearing, and
  # Terraform assigns that empty string rather than treating the variable as
  # unset — so this has to be gated on enable_gpu_render_vm. A plain
  # "null or >= 12" rule fails the plan for the *entire* config, hosting VM
  # included, on any repo that hasn't set the secret yet.
  #
  # try() rather than a null guard because length(null) is an error in its own
  # right, which surfaces as "Error in function call" instead of this message.
  # 12 is Azure's own minimum for a Windows admin password.
  validation {
    condition     = !var.enable_gpu_render_vm || try(length(var.gpu_render_admin_password), 0) >= 12
    error_message = "gpu_render_admin_password must be at least 12 characters when enable_gpu_render_vm is true. An empty value usually means the GPU_RENDER_ADMIN_PASSWORD repository secret is not set."
  }
}

resource "azurerm_windows_virtual_machine" "gpu_render" {
  count                 = var.enable_gpu_render_vm ? 1 : 0
  name                  = "vm-${local.name_prefix}-gpu-render"
  location              = azurerm_resource_group.this.location
  resource_group_name   = azurerm_resource_group.this.name
  size                  = var.gpu_render_vm_size
  admin_username        = var.admin_username
  admin_password        = var.gpu_render_admin_password
  network_interface_ids = [azurerm_network_interface.gpu_render[0].id]
  tags                  = local.common_tags

  # Windows Server 2022 is on Microsoft's supported list for the NVv4 AMD
  # driver. Windows 11 would also work but needs a client licence for no
  # benefit here — nothing about this box is interactive except maintenance.
  #
  # Deliberately a stock marketplace image and not a captured golden image:
  # azurerm_windows_virtual_machine always emits an osProfile, and Azure
  # rejects that when creating from a *specialized* image — which is the only
  # kind that survives Steam's machine-bound login. The golden image is
  # therefore a restore artifact captured out-of-band by
  # scripts/capture-golden-image.sh, not an input to this resource.
  source_image_reference {
    publisher = "MicrosoftWindowsServer"
    offer     = "WindowsServer"
    sku       = "2022-datacenter-g2"
    version   = "latest"
  }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Premium_LRS"
    # Windows Server takes ~30 GB and CS2 another ~35 GB, so the 127 GB
    # default is adequate but not roomy. Premium rather than StandardSSD
    # because this disk is the only thing billed while the VM is deallocated
    # and it is also what start-up latency rides on; the delta is ~EUR 9/month.
    #
    # Scratch — demo downloads, frame spill — belongs on the 88 GiB ephemeral
    # D: drive instead, which is wiped on deallocate and costs nothing.
    disk_size_gb = 128
  }

  # Needed to write finished clips to blob storage and to deallocate itself
  # when the render queue drains. Both role assignments are below.
  identity {
    type = "SystemAssigned"
  }
}

# The supported way to get the Radeon Instinct MI25 working on NVv4/Windows.
# Microsoft publishes the only drivers supported on these VMs; installing one
# from anywhere else is unsupported.
resource "azurerm_virtual_machine_extension" "gpu_render_amd_driver" {
  count                      = var.enable_gpu_render_vm ? 1 : 0
  name                       = "AmdGpuDriverWindows"
  virtual_machine_id         = azurerm_windows_virtual_machine.gpu_render[0].id
  publisher                  = "Microsoft.HpcCompute"
  type                       = "AmdGpuDriverWindows"
  type_handler_version       = "1.0"
  auto_upgrade_minor_version = true
  tags                       = local.common_tags
}

# The Windows counterpart of cloud-init/hosting.yaml.tftpl. Windows has no
# cloud-init: custom_data on a Windows VM is written to disk and never
# executed, so the extension is the mechanism.
#
# The script is embedded rather than fetched from a URL, so an apply depends on
# nothing but this repo. textencodebase64 with UTF-16LE is required —
# PowerShell's -EncodedCommand rejects UTF-8 base64, and Terraform's plain
# base64encode produces UTF-8.
#
# It goes in `settings` (public) and not `protected_settings` on purpose: the
# script holds no secrets, and being able to read it back off the VM is worth
# more than hiding it. The Steam login is done by hand once — see the ADR.
resource "azurerm_virtual_machine_extension" "gpu_render_bootstrap" {
  count                      = var.enable_gpu_render_vm ? 1 : 0
  name                       = "bootstrap"
  virtual_machine_id         = azurerm_windows_virtual_machine.gpu_render[0].id
  publisher                  = "Microsoft.Compute"
  type                       = "CustomScriptExtension"
  type_handler_version       = "1.10"
  auto_upgrade_minor_version = true
  tags                       = local.common_tags

  # The driver first, so the script can assert the GPU is present.
  depends_on = [azurerm_virtual_machine_extension.gpu_render_amd_driver]

  settings = jsonencode({
    commandToExecute = "powershell.exe -ExecutionPolicy Unrestricted -NoProfile -EncodedCommand ${textencodebase64(file("${path.module}/scripts/gpu-render-bootstrap.ps1"), "UTF-16LE")}"
  })
}

# --- Permissions the VM needs on the rest of the subscription ---------------
#
# NOTE: creating role assignments requires more than Contributor. The CI
# service principal is granted Contributor at subscription scope by
# bootstrap/bootstrap-tfstate.sh, which is NOT enough — it also needs "Role
# Based Access Control Administrator" (or Owner), or these two resources fail
# with AuthorizationFailed. Grant it once, by hand, before enabling this VM.

resource "azurerm_role_assignment" "gpu_render_clips_writer" {
  count                = var.enable_gpu_render_vm && var.enable_blob_storage ? 1 : 0
  scope                = azurerm_storage_account.clips[0].id
  role_definition_name = "Storage Blob Data Contributor"
  principal_id         = azurerm_windows_virtual_machine.gpu_render[0].identity[0].principal_id
}

# So the box can deallocate itself once the queue drains, without the backend
# having to notice it went idle. Virtual Machine Contributor is broader than
# the single deallocate/action this needs — a custom role definition would be
# tighter, and is worth doing if this VM ever handles anything but renders.
resource "azurerm_role_assignment" "gpu_render_self_deallocate" {
  count                = var.enable_gpu_render_vm ? 1 : 0
  scope                = azurerm_windows_virtual_machine.gpu_render[0].id
  role_definition_name = "Virtual Machine Contributor"
  principal_id         = azurerm_windows_virtual_machine.gpu_render[0].identity[0].principal_id
}

# Backstop, not the mechanism. The orchestrator deallocates the VM when the
# queue drains; this catches the case where it doesn't — a crashed agent, or a
# maintenance RDP session someone walked away from. Nightly, because a render
# that is still going at 03:00 has already gone wrong.
resource "azurerm_dev_test_global_vm_shutdown_schedule" "gpu_render" {
  count                 = var.enable_gpu_render_vm ? 1 : 0
  virtual_machine_id    = azurerm_windows_virtual_machine.gpu_render[0].id
  location              = azurerm_resource_group.this.location
  enabled               = true
  daily_recurrence_time = "0300"
  timezone              = "W. Europe Standard Time"
  tags                  = local.common_tags

  notification_settings {
    enabled = false
  }
}
