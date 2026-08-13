# Azure Function App for the demo-rendering pipeline (later phase). Modeled
# now, disabled by default (enable_function_app = false). Needs
# enable_blob_storage = true too, since it uses the same storage account
# both for its own runtime state and as the destination for rendered clips.

resource "azurerm_service_plan" "function_app" {
  count               = var.enable_function_app ? 1 : 0
  name                = "asp-${local.name_prefix}-func"
  resource_group_name = azurerm_resource_group.this.name
  location            = azurerm_resource_group.this.location
  os_type             = "Linux"
  sku_name            = "Y1" # Consumption plan — scales to zero between demo-render triggers
  tags                = local.common_tags
}

resource "azurerm_linux_function_app" "demo_renderer" {
  count               = var.enable_function_app ? 1 : 0
  name                = "func-${local.name_prefix}-demo-renderer"
  resource_group_name = azurerm_resource_group.this.name
  location            = azurerm_resource_group.this.location
  service_plan_id     = azurerm_service_plan.function_app[0].id

  storage_account_name       = azurerm_storage_account.clips[0].name
  storage_account_access_key = azurerm_storage_account.clips[0].primary_access_key

  site_config {}

  tags = local.common_tags
}
