# Transitional. Delete after one successful apply.
#
# Every resource here used to be wrapped in `count = var.enable_* ? 1 : 0`,
# which meant Terraform tracked it in state as `address[0]`. Removing the
# toggles changes the address to plain `address` — and to Terraform an address
# that vanished is a resource to DESTROY, while the new one is a resource to
# CREATE. Applied without these blocks, that would have destroyed the live
# hosting VM, and with it the Postgres volume and Caddy's certificates.
#
# `moved` blocks say "this is the same object under a new name" declaratively,
# so the plan shows a move rather than a replacement and nothing has to be
# fixed up by hand with `terraform state mv`. A block whose `from` address
# isn't in state is silently ignored, so the storage entries below are correct
# whether or not the storage apply had already run when this landed.
#
# The GPU render VM has no entries: it has never been applied, so there is no
# old address to move from.

moved {
  from = azurerm_virtual_network.hosting[0]
  to   = azurerm_virtual_network.hosting
}

moved {
  from = azurerm_subnet.hosting[0]
  to   = azurerm_subnet.hosting
}

moved {
  from = azurerm_public_ip.hosting[0]
  to   = azurerm_public_ip.hosting
}

moved {
  from = azurerm_network_security_group.hosting[0]
  to   = azurerm_network_security_group.hosting
}

moved {
  from = azurerm_network_interface.hosting[0]
  to   = azurerm_network_interface.hosting
}

moved {
  from = azurerm_network_interface_security_group_association.hosting[0]
  to   = azurerm_network_interface_security_group_association.hosting
}

moved {
  from = azurerm_linux_virtual_machine.hosting[0]
  to   = azurerm_linux_virtual_machine.hosting
}

# The storage account's name embeds this random suffix, so losing it would
# rename the account — which means destroying it and everything in it.
moved {
  from = random_string.storage_suffix[0]
  to   = random_string.storage_suffix
}

moved {
  from = azurerm_storage_account.clips[0]
  to   = azurerm_storage_account.clips
}

moved {
  from = azurerm_storage_container.clips[0]
  to   = azurerm_storage_container.clips
}

moved {
  from = azurerm_storage_container.demos[0]
  to   = azurerm_storage_container.demos
}

moved {
  from = azurerm_storage_management_policy.clips[0]
  to   = azurerm_storage_management_policy.clips
}
