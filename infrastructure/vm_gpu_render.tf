# GPU VM that runs a real CS2 client against a demo file and records highlight
# clips with HLAE. At EUR 0.4476/hr this is by far the most expensive resource
# in the config — running continuously it is ~EUR 320/month, so the cost model
# depends entirely on it being deallocated when idle.
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
  name                = "pip-${local.name_prefix}-gpu-render"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  allocation_method   = "Static"
  sku                 = "Standard"
  tags                = local.common_tags
}

resource "azurerm_network_security_group" "gpu_render" {
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
  name                = "nic-${local.name_prefix}-gpu-render"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = local.common_tags

  ip_configuration {
    name = "internal"
    # Reuses the hosting VNet/subnet, so this VM cannot exist without the
    # hosting VM. That is fine — the backend that will drive it lives there,
    # and a second VNet buys nothing while both boxes are in one region.
    subnet_id                     = azurerm_subnet.hosting.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.gpu_render.id
  }
}

resource "azurerm_network_interface_security_group_association" "gpu_render" {
  network_interface_id      = azurerm_network_interface.gpu_render.id
  network_security_group_id = azurerm_network_security_group.gpu_render.id
}

variable "gpu_render_admin_password" {
  description = "Local admin password for the GPU render VM. Windows VMs require one."
  type        = string
  sensitive   = true

  # FROM CI SECRET: GPU_RENDER_ADMIN_PASSWORD.
  #
  # The one value here that genuinely cannot live in terraform.tfvars. It is a
  # live credential for a box with RDP exposed, and this repo is public. No
  # default, so a missing value fails the plan rather than quietly applying
  # something guessable.
  #
  # try() rather than a null guard because length(null) is an error in its own
  # right, and surfaces as "Error in function call" instead of this message.
  # An unset GitHub secret interpolates to "" rather than disappearing, so the
  # empty case is the one actually worth naming. 12 is Azure's own minimum.
  validation {
    condition     = try(length(var.gpu_render_admin_password), 0) >= 12
    error_message = "gpu_render_admin_password must be at least 12 characters. An empty value usually means the GPU_RENDER_ADMIN_PASSWORD repository secret is not set."
  }
}

resource "azurerm_windows_virtual_machine" "gpu_render" {
  name                = "vm-${local.name_prefix}-gpu-render"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  size                = var.gpu_render_vm_size
  admin_username      = var.admin_username
  admin_password      = var.gpu_render_admin_password

  # Windows computer names are NetBIOS names and cap at 15 characters, so the
  # Azure resource name can't be reused as the default the way it is on the
  # Linux box — "vm-fragvault-prod-gpu-render" is 28. The provider only checks
  # this at apply time, so a plan will happily pass without it.
  #
  # This lands in the specialized golden image, so changing it later means
  # recreating both the VM and the image. "fv-prod-gpu" is 11.
  computer_name = substr("fv-${var.environment}-gpu", 0, 15)

  network_interface_ids = [azurerm_network_interface.gpu_render.id]
  tags                  = local.common_tags

  # Same provider quirk as the hosting VM — see the comment there.
  vm_agent_platform_updates_enabled = true

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
  name                       = "AmdGpuDriverWindows"
  virtual_machine_id         = azurerm_windows_virtual_machine.gpu_render.id
  publisher                  = "Microsoft.HpcCompute"
  type                       = "AmdGpuDriverWindows"
  type_handler_version       = "1.0"
  auto_upgrade_minor_version = true
  tags                       = local.common_tags
}

# The Windows counterpart of cloud-init/hosting.yaml.tftpl. Windows has no
# cloud-init: custom_data on a Windows VM is written to disk and never
# executed, so something has to run the script for us.
#
# Run Command and NOT CustomScriptExtension. CSE takes its script as a
# `commandToExecute` string that the guest agent hands to cmd.exe, and cmd.exe
# caps a command line at 8191 characters. The script base64-encoded as UTF-16LE
# (which -EncodedCommand requires) came to 21128, so the extension failed with
# "The command line is too long." before PowerShell was ever launched — a
# failure that looks like a broken script but never executed a line of one.
# Trimming the script under the cap would only defer this.
#
# Run Command ships the script in the request body instead; the agent writes it
# to disk and invokes PowerShell on the file, so there is no command line to
# overflow and no encoding dance. It stays embedded from the repo rather than
# fetched from a URL, so an apply still depends on nothing but this checkout.
#
# `source.script` is stored in plain text and readable from the portal, which
# is the same trade CSE's public `settings` made and is still the right one:
# the script holds no secrets, and reading it back off the VM is worth more
# than hiding it. The Steam login is done by hand once — see the ADR.
#
# What CSE's UTF-16LE base64 was quietly buying, and this is not: an explicit
# encoding. Run Command writes the script to disk and lets PowerShell open it,
# and Windows PowerShell 5.1 decodes a BOM-less file as Windows-1252, so any
# non-ASCII byte in the script is corrupted before it is ever parsed. Hence the
# precondition below and the ASCII rule at the top of the script itself.
#
# A failed script does fail the apply: the run command surfaces a non-zero exit
# as VMExtensionProvisioningError and Terraform errors on it. The error carries
# only the first parse or runtime failure, though — for anything past that, read
# the transcript at C:\fragvault\logs\bootstrap.log on the VM.
resource "azurerm_virtual_machine_run_command" "gpu_render_bootstrap" {
  name               = "bootstrap"
  location           = azurerm_resource_group.this.location
  virtual_machine_id = azurerm_windows_virtual_machine.gpu_render.id
  tags               = local.common_tags

  # The driver first, so the script can assert the GPU is present.
  depends_on = [azurerm_virtual_machine_extension.gpu_render_amd_driver]

  source {
    script = file("${path.module}/scripts/gpu-render-bootstrap.ps1")
  }

  # How long TERRAFORM waits, not how long the script may run — azurerm 3.117
  # does not expose the run command's own timeoutInSeconds, so the script is
  # bounded by the platform default whatever we put here. Raised from the 30m
  # default because a cold run is the slow case: Chocolatey pulls ffmpeg, 7zip,
  # sysinternals and the whole Steam client, and HLAE comes from GitHub. If
  # Terraform gives up first the script keeps running on the VM regardless, so
  # a timeout here means "go read the transcript", not "it died".
  timeouts {
    create = "60m"
    update = "60m"
  }

  # Turns a corrupted-encoding bug into a plan failure with a name. Without it
  # the only symptom is a PowerShell parse error on the VM, reported against a
  # line that is nowhere near the character that caused it.
  lifecycle {
    precondition {
      condition     = can(regex("^[[:ascii:]]*$", file("${path.module}/scripts/gpu-render-bootstrap.ps1")))
      error_message = "gpu-render-bootstrap.ps1 must be pure ASCII. Windows PowerShell reads the BOM-less file Run Command writes as Windows-1252, so an em dash or smart quote becomes a stray quotation mark and breaks parsing. Replace the character with its ASCII equivalent."
    }
  }
}

# --- Permissions the VM needs on the rest of the subscription ---------------
#
# NOTE: creating role assignments requires more than Contributor. The CI
# service principal is granted Contributor at subscription scope by
# bootstrap/bootstrap-tfstate.sh, which is NOT enough — it also needs "Role
# Based Access Control Administrator" (or Owner), or these two resources fail
# with AuthorizationFailed. Grant it once, by hand, before enabling this VM.

resource "azurerm_role_assignment" "gpu_render_clips_writer" {
  scope                = azurerm_storage_account.clips.id
  role_definition_name = "Storage Blob Data Contributor"
  principal_id         = azurerm_windows_virtual_machine.gpu_render.identity[0].principal_id
}

# So the box can deallocate itself once the queue drains, without the backend
# having to notice it went idle. Virtual Machine Contributor is broader than
# the single deallocate/action this needs — a custom role definition would be
# tighter, and is worth doing if this VM ever handles anything but renders.
resource "azurerm_role_assignment" "gpu_render_self_deallocate" {
  scope                = azurerm_windows_virtual_machine.gpu_render.id
  role_definition_name = "Virtual Machine Contributor"
  principal_id         = azurerm_windows_virtual_machine.gpu_render.identity[0].principal_id
}

# Backstop, not the mechanism. The orchestrator deallocates the VM when the
# queue drains; this catches the case where it doesn't — a crashed agent, or a
# maintenance RDP session someone walked away from. Nightly, because a render
# that is still going at 03:00 has already gone wrong.
resource "azurerm_dev_test_global_vm_shutdown_schedule" "gpu_render" {
  virtual_machine_id    = azurerm_windows_virtual_machine.gpu_render.id
  location              = azurerm_resource_group.this.location
  enabled               = true
  daily_recurrence_time = "0300"
  timezone              = "W. Europe Standard Time"
  tags                  = local.common_tags

  notification_settings {
    enabled = false
  }
}
