# Blob storage for rendered highlight clips. Not used until the
# demo-rendering phase exists, but modeled now per the full architecture —
# disabled by default (enable_blob_storage = false) so it isn't provisioned
# before anything writes to it. Storage account names must be globally
# unique, lowercase, alphanumeric only, <=24 chars.

resource "random_string" "storage_suffix" {
  count   = var.enable_blob_storage ? 1 : 0
  length  = 6
  special = false
  upper   = false
}

resource "azurerm_storage_account" "clips" {
  count                    = var.enable_blob_storage ? 1 : 0
  name                     = substr("st${replace(var.project_name, "-", "")}${random_string.storage_suffix[0].result}", 0, 24)
  resource_group_name      = azurerm_resource_group.this.name
  location                 = azurerm_resource_group.this.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
  min_tls_version          = "TLS1_2"
  tags                     = local.common_tags
}

resource "azurerm_storage_container" "clips" {
  count                 = var.enable_blob_storage ? 1 : 0
  name                  = "clips"
  storage_account_name  = azurerm_storage_account.clips[0].name
  container_access_type = "private" # served to users via SAS URLs, not public listing
}
