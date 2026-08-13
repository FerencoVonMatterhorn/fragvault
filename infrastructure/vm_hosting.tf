# Small Linux VM hosting the Phase 1 frontend (static build) + backend (Go
# systemd service) behind nginx. See /docs/architecture.md for the topology.

resource "azurerm_virtual_network" "hosting" {
  count               = var.enable_hosting_vm ? 1 : 0
  name                = "vnet-${local.name_prefix}-hosting"
  address_space       = ["10.10.0.0/16"]
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = local.common_tags
}

resource "azurerm_subnet" "hosting" {
  count                = var.enable_hosting_vm ? 1 : 0
  name                 = "snet-hosting"
  resource_group_name  = azurerm_resource_group.this.name
  virtual_network_name = azurerm_virtual_network.hosting[0].name
  address_prefixes     = ["10.10.1.0/24"]
}

resource "azurerm_public_ip" "hosting" {
  count               = var.enable_hosting_vm ? 1 : 0
  name                = "pip-${local.name_prefix}-hosting"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  allocation_method   = "Static"
  sku                 = "Standard"
  tags                = local.common_tags
}

resource "azurerm_network_security_group" "hosting" {
  count               = var.enable_hosting_vm ? 1 : 0
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
  count               = var.enable_hosting_vm ? 1 : 0
  name                = "nic-${local.name_prefix}-hosting"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = local.common_tags

  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.hosting[0].id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.hosting[0].id
  }
}

resource "azurerm_network_interface_security_group_association" "hosting" {
  count                     = var.enable_hosting_vm ? 1 : 0
  network_interface_id      = azurerm_network_interface.hosting[0].id
  network_security_group_id = azurerm_network_security_group.hosting[0].id
}

resource "azurerm_linux_virtual_machine" "hosting" {
  count                 = var.enable_hosting_vm ? 1 : 0
  name                  = "vm-${local.name_prefix}-hosting"
  location              = azurerm_resource_group.this.location
  resource_group_name   = azurerm_resource_group.this.name
  size                  = var.hosting_vm_size
  admin_username        = var.admin_username
  network_interface_ids = [azurerm_network_interface.hosting[0].id]
  tags                  = local.common_tags

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

  # Deliberately no custom_data / cloud-init app deployment here yet — nginx
  # config, the Go binary, and the systemd unit get deployed via the
  # GitHub Actions CI pipeline once the repo is live, not baked into the
  # image. Keeps this VM cheap to recreate and the deploy path identical to
  # every later app update.
}
