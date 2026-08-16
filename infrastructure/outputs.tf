output "resource_group_name" {
  value = azurerm_resource_group.this.name
}

output "hosting_vm_public_ip" {
  description = "Point your domain's A record at this once the VM exists."
  value       = azurerm_public_ip.hosting.ip_address
}

output "blob_storage_account_name" {
  value = azurerm_storage_account.clips.name
}

output "clips_container_name" {
  value = azurerm_storage_container.clips.name
}

output "demos_container_name" {
  description = "Retained .dem files. The backend needs a SAS scoped to this container; see README."
  value       = azurerm_storage_container.demos.name
}

output "gpu_render_vm_public_ip" {
  description = "RDP here for maintenance. Rendering runs in the auto-logon console session — don't connect while a render is going."
  value       = azurerm_public_ip.gpu_render.ip_address
}

output "gpu_render_vm_name" {
  description = "What `az vm start` / `az vm deallocate` need. The whole cost model depends on this box being deallocated when idle."
  value       = azurerm_windows_virtual_machine.gpu_render.name
}
