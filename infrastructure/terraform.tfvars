# This deployment's values. Committed on purpose: there is exactly one
# environment, nothing here is secret, and a value you can read in a diff beats
# a value you have to click through repository settings to find.
#
# Auto-loaded because of the filename — CI runs Terraform from this directory,
# so nothing has to pass -var-file.
#
# The reasoning behind each choice lives in variables.tf, next to the variable
# it constrains. Two variables are deliberately absent and come from CI
# secrets instead: ssh_public_key and allowed_ssh_source_ip. So does
# gpu_render_admin_password, which is an actual credential and must never
# appear in this file.

project_name = "fragvault"
environment  = "prod"
location     = "swedencentral"

# Hosting VM — frontend, backend, Postgres, Caddy.
hosting_vm_size = "Standard_B2ats_v2"
admin_username  = "fragvault"

# GPU render VM. Retires 2026-09-30 — see the warning in variables.tf.
gpu_render_vm_size = "Standard_NV4as_v4"
