#!/usr/bin/env bash
#
# Captures the configured GPU render VM into an Azure Compute Gallery image, so
# the box can be rebuilt without repeating the manual Steam login and the ~56 GB
# CS2 download.
#
# Why a script and not Terraform: an image version is captured *from a running
# machine's disk*, which is state Terraform has no way to express. Same reason
# bootstrap-tfstate.sh exists. The source of truth for what the machine
# contains stays gpu-render-bootstrap.ps1 plus the manual steps in
# docs/adr-003-render-vm.md; this is a restore artifact, not a definition.
#
# The image is captured SPECIALIZED (no sysprep) on purpose. Generalizing
# resets the machine SID, which invalidates Steam's machine-bound sentry file
# and forces a fresh Steam Guard challenge on every rebuild — defeating the
# entire point of capturing it. The cost is that every VM restored from this
# image carries the same computer name and SID, which is fine for a
# single-purpose appliance and would not be for a fleet.
#
# Consequence worth knowing: azurerm_windows_virtual_machine cannot create from
# a specialized image (it always emits an osProfile, which Azure rejects), so
# restoring is the `az vm create` at the bottom of this file, by hand, followed
# by `terraform import`.
#
# Requires: az CLI, logged in to the FragVault subscription as someone who can
# create galleries and read the VM.
#
# Usage:
#   ./capture-golden-image.sh                  # captures version 1.0.0
#   IMAGE_VERSION=1.1.0 ./capture-golden-image.sh
#
# On Windows run this from Git Bash, or use Azure Cloud Shell.

set -euo pipefail

# Git Bash rewrites arguments that look like Unix paths, so an ARM resource id
# becomes C:/Program Files/Git/subscriptions/... and Azure reports a misleading
# MissingSubscription. Same opt-out as bootstrap-tfstate.sh.
export MSYS2_ARG_CONV_EXCL='/subscriptions/'

LOCATION="${LOCATION:-swedencentral}"
RG_NAME="${RG_NAME:-rg-fragvault-prod}"
VM_NAME="${VM_NAME:-vm-fragvault-prod-gpu-render}"
# Gallery names allow only alphanumerics, periods and underscores — no hyphens,
# unlike every other resource here.
GALLERY_NAME="${GALLERY_NAME:-gal_fragvault_prod}"
IMAGE_DEF_NAME="${IMAGE_DEF_NAME:-cs2-render}"
IMAGE_VERSION="${IMAGE_VERSION:-1.0.0}"

echo "==> Capturing $VM_NAME into $GALLERY_NAME/$IMAGE_DEF_NAME:$IMAGE_VERSION"

# The VM must be deallocated: capturing a running machine's OS disk gets you a
# crash-consistent image, and Steam in particular does not enjoy being restored
# mid-write.
echo "==> Deallocating the VM (a capture of a running machine is crash-consistent, not clean)"
az vm deallocate --resource-group "$RG_NAME" --name "$VM_NAME"

VM_ID="$(az vm show --resource-group "$RG_NAME" --name "$VM_NAME" --query id --output tsv)"
echo "==> VM id: $VM_ID"

if az sig show --resource-group "$RG_NAME" --gallery-name "$GALLERY_NAME" >/dev/null 2>&1; then
  echo "==> Gallery $GALLERY_NAME already exists"
else
  echo "==> Creating gallery $GALLERY_NAME"
  az sig create \
    --resource-group "$RG_NAME" \
    --gallery-name "$GALLERY_NAME" \
    --location "$LOCATION"
fi

if az sig image-definition show \
  --resource-group "$RG_NAME" \
  --gallery-name "$GALLERY_NAME" \
  --gallery-image-definition "$IMAGE_DEF_NAME" >/dev/null 2>&1; then
  echo "==> Image definition $IMAGE_DEF_NAME already exists"
else
  echo "==> Creating image definition $IMAGE_DEF_NAME (Specialized, V2)"
  az sig image-definition create \
    --resource-group "$RG_NAME" \
    --gallery-name "$GALLERY_NAME" \
    --gallery-image-definition "$IMAGE_DEF_NAME" \
    --publisher fragvault \
    --offer cs2-render \
    --sku win2022 \
    --os-type Windows \
    --os-state Specialized \
    --hyper-v-generation V2 \
    --location "$LOCATION"
fi

echo "==> Creating image version $IMAGE_VERSION (this takes a while — it copies ~83 GB)"
az sig image-version create \
  --resource-group "$RG_NAME" \
  --gallery-name "$GALLERY_NAME" \
  --gallery-image-definition "$IMAGE_DEF_NAME" \
  --gallery-image-version "$IMAGE_VERSION" \
  --virtual-machine "$VM_ID" \
  --location "$LOCATION"

IMAGE_VERSION_ID="$(az sig image-version show \
  --resource-group "$RG_NAME" \
  --gallery-name "$GALLERY_NAME" \
  --gallery-image-definition "$IMAGE_DEF_NAME" \
  --gallery-image-version "$IMAGE_VERSION" \
  --query id --output tsv)"

cat <<EOF

==> Done.

Image version id:
  $IMAGE_VERSION_ID

The VM is left deallocated. Start it again with:
  az vm start --resource-group $RG_NAME --name $VM_NAME

To rebuild from this image after losing the VM — note there are no admin
credentials, because a specialized image already contains its accounts:

  az vm create \\
    --resource-group $RG_NAME \\
    --name $VM_NAME \\
    --image "$IMAGE_VERSION_ID" \\
    --specialized \\
    --size <gpu_render_vm_size from infrastructure/terraform.tfvars> \\
    --nics nic-fragvault-prod-gpu-render

Take the size from terraform.tfvars rather than copying it from here: it is the
single place that SKU is defined, and Standard_NV4as_v4 retires 2026-09-30. A
restore onto the wrong size is a VM Terraform immediately wants to change.

then bring it back under Terraform:

  terraform import azurerm_windows_virtual_machine.gpu_render \\
    /subscriptions/<sub>/resourceGroups/$RG_NAME/providers/Microsoft.Compute/virtualMachines/$VM_NAME

Storage for the image version is billed as a snapshot — roughly EUR 5-9/month
for a ~83 GB image. Delete old versions you are not restoring from.
EOF
