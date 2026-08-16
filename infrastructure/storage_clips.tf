# Blob storage for the render pipeline: retained demo files in, rendered
# highlight clips out. Storage account names must be globally unique,
# lowercase, alphanumeric only, <=24 chars.

resource "random_string" "storage_suffix" {
  length  = 6
  special = false
  upper   = false
}

resource "azurerm_storage_account" "clips" {
  name                     = substr("st${replace(var.project_name, "-", "")}${random_string.storage_suffix.result}", 0, 24)
  resource_group_name      = azurerm_resource_group.this.name
  location                 = azurerm_resource_group.this.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
  min_tls_version          = "TLS1_2"
  tags                     = local.common_tags

  # Unlike the tfstate account, shared key access stays ON here. The backend
  # uploads demos with a container-scoped SAS held in /opt/fragvault/.env, and
  # an account-key SAS is what makes that possible without giving the hosting
  # VM a managed identity and an Entra round-trip. Swap to a user-delegation
  # SAS when the hosting VM gets an identity.
  shared_access_key_enabled       = true
  allow_nested_items_to_be_public = false
}

# Rendered highlight clips. Served to users via short-lived SAS URLs, never by
# public listing.
resource "azurerm_storage_container" "clips" {
  name                  = "clips"
  storage_account_name  = azurerm_storage_account.clips.name
  container_access_type = "private"
}

# Retained .dem files. The analysis worker deletes its local copy the moment
# parsing finishes, and demo_analyses.demo_url points at a Valve URL that
# expires after a few weeks — so without this container a highlight can only
# ever be rendered in the window right after it was analysed.
resource "azurerm_storage_container" "demos" {
  name                  = "demos"
  storage_account_name  = azurerm_storage_account.clips.name
  container_access_type = "private"
}

# Demos are large (capped at 800 MiB by the downloader) and cold almost
# immediately: a demo is read once at analysis time, then only again if someone
# renders a clip from that match. Cool after a week pays for itself; archive is
# deliberately avoided because rehydration takes hours and would turn a render
# into an overnight job.
resource "azurerm_storage_management_policy" "clips" {
  storage_account_id = azurerm_storage_account.clips.id

  rule {
    name    = "demos-cool-then-keep"
    enabled = true

    filters {
      prefix_match = ["demos/"]
      blob_types   = ["blockBlob"]
    }

    actions {
      base_blob {
        tier_to_cool_after_days_since_modification_greater_than = 7
      }
    }
  }
}
