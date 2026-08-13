# GPU VM that runs actual CS2 (via the golden image described in the
# project brief) to render highlight clips from demo files. Later phase,
# disabled by default (enable_gpu_render_vm = false) — this is the most
# expensive resource in this whole config, don't flip it on until the
# rendering pipeline is ready to use it. GPU SKUs also need quota approval
# in most Azure subscriptions/regions before they can be created at all;
# check that before enabling.

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
  # desktop/game session, not a public web service.
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
    name                          = "internal"
    subnet_id                     = azurerm_subnet.hosting[0].id # reuses the hosting VNet/subnet for Phase 1 simplicity
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.gpu_render[0].id
  }
}

resource "azurerm_network_interface_security_group_association" "gpu_render" {
  count                     = var.enable_gpu_render_vm ? 1 : 0
  network_interface_id      = azurerm_network_interface.gpu_render[0].id
  network_security_group_id = azurerm_network_security_group.gpu_render[0].id
}

# NOTE: this references a golden image (CS2 + Steam credentials
# pre-installed, per the project description) rather than a stock
# marketplace image. Building/capturing that image (e.g. via Azure Image
# Builder or a manual sysprep+capture into a Shared Image Gallery) is
# separate work not yet done — source_image_id below is a placeholder
# variable to fill in once that image exists.
variable "gpu_render_golden_image_id" {
  description = "Resource ID of the captured golden image (CS2 + Steam) in a Shared Image Gallery. Required once enable_gpu_render_vm = true."
  type        = string
  default     = ""
}

variable "gpu_render_admin_password" {
  description = "Local admin password for the GPU render VM. Windows VMs require one. Pass via CI secret / a tfvars file that's gitignored — never commit a real value. Left with no default on purpose so `terraform apply` fails loudly instead of silently applying a placeholder password."
  type        = string
  sensitive   = true
  default     = null
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

  source_image_id = var.gpu_render_golden_image_id

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Premium_LRS"
  }
}
