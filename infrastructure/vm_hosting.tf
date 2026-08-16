# Small Linux VM hosting the Phase 1 frontend (static build) + backend (Go
# systemd service) behind nginx, which reverse-proxies /api and /auth to the
# backend and serves the built frontend as static files.

resource "azurerm_virtual_network" "hosting" {
  name                = "vnet-${local.name_prefix}-hosting"
  address_space       = ["10.10.0.0/16"]
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = local.common_tags
}

resource "azurerm_subnet" "hosting" {
  name                 = "snet-hosting"
  resource_group_name  = azurerm_resource_group.this.name
  virtual_network_name = azurerm_virtual_network.hosting.name
  address_prefixes     = ["10.10.1.0/24"]
}

resource "azurerm_public_ip" "hosting" {
  name                = "pip-${local.name_prefix}-hosting"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  allocation_method   = "Static"
  sku                 = "Standard"
  tags                = local.common_tags
}

resource "azurerm_network_security_group" "hosting" {
  name                = "nsg-${local.name_prefix}-hosting"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = local.common_tags

  security_rule {
    name                       = "AllowHTTP"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "80"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "AllowHTTPS"
    priority                   = 110
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "443"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "AllowSSHFromAdmin"
    priority                   = 120
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "22"
    source_address_prefix      = var.allowed_ssh_source_ip
    destination_address_prefix = "*"
  }
}

resource "azurerm_network_interface" "hosting" {
  name                = "nic-${local.name_prefix}-hosting"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = local.common_tags

  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.hosting.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.hosting.id
  }
}

resource "azurerm_network_interface_security_group_association" "hosting" {
  network_interface_id      = azurerm_network_interface.hosting.id
  network_security_group_id = azurerm_network_security_group.hosting.id
}

resource "azurerm_linux_virtual_machine" "hosting" {
  name                  = "vm-${local.name_prefix}-hosting"
  location              = azurerm_resource_group.this.location
  resource_group_name   = azurerm_resource_group.this.name
  size                  = var.hosting_vm_size
  admin_username        = var.admin_username
  network_interface_ids = [azurerm_network_interface.hosting.id]
  tags                  = local.common_tags

  # Set explicitly only to stop a perpetual `true -> false` in every plan. The
  # provider declares this Optional with `Default: false` and no Computed, so
  # omitting it makes Terraform read the config as an actual `false` — while
  # Azure turns platform updates on by itself and reports `true`. The diff can
  # never converge, and it follows the config rather than any one machine's
  # state, so it shows up for everyone. Matching Azure is also what we want:
  # `false` would mean asking Azure to stop updating the VM agent. Not
  # ForceNew, so this is an in-place no-op and does not touch the VM.
  vm_agent_platform_updates_enabled = true

  admin_ssh_key {
    username   = var.admin_username
    public_key = var.ssh_public_key
  }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "0001-com-ubuntu-server-jammy"
    sku       = "22_04-lts-gen2"
    version   = "latest"
  }

  # cloud-init installs Docker on first boot so the VM is ready to run
  # containers the moment it exists. Only the runtime is baked in — the
  # application itself still arrives via CI, so recreating this VM stays
  # cheap and the deploy path is identical for every later update.
  #
  # Editing the template replaces the VM. That's intended: it's an image
  # definition, and drifting it in place would make "recreate the VM" stop
  # producing the same machine.
  custom_data = base64encode(templatefile("${path.module}/cloud-init/hosting.yaml.tftpl", {
    admin_username = var.admin_username
  }))
}
