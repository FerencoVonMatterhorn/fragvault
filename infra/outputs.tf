output "resource_group_name" {
  value = azurerm_resource_group.this.name
}

output "hosting_vm_public_ip" {
  description = "Point your domain's A record at this once the VM exists."
  value       = var.enable_hosting_vm ? azurerm_public_ip.hosting[0].ip_address : null
}

output "blob_storage_account_name" {
  value = var.enable_blob_storage ? azurerm_storage_account.clips[0].name : null
}

output "clips_container_name" {
  value = var.enable_blob_storage ? azurerm_storage_container.clips[0].name : null
}

output "function_app_default_hostname" {
  value = var.enable_function_app ? azurerm_linux_function_app.demo_renderer[0].default_hostname : null
}

output "gpu_render_vm_public_ip" {
  value = var.enable_gpu_render_vm ? azurerm_public_ip.gpu_render[0].ip_address : null
}
