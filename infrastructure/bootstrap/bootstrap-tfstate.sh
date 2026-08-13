#!/usr/bin/env bash
#
# Creates the Azure Storage account that holds Terraform remote state, and
# grants CI the two roles it needs: data-plane access to the state blob, and
# a control-plane role on the subscription to create infrastructure with.
#
# Why a script and not Terraform: this is the chicken-and-egg resource. A
# storage account managed by the same state it stores is a footgun — a
# `terraform destroy` (or a corrupted state) takes the state with it. So it
# lives outside Terraform, in its own resource group, created once by hand.
# The script is idempotent: re-running it is safe and changes nothing.
#
# Requires: az CLI, `az login` as someone who can create resource groups and
# assign roles (Owner or User Access Administrator) in the target subscription.
#
# Usage:
#   ./bootstrap-tfstate.sh                       # uses the defaults below
#   CI_CLIENT_ID=<app-id> ./bootstrap-tfstate.sh # also grants CI data access
#
# On Windows run this from Git Bash, or use Azure Cloud Shell.

set -euo pipefail

# Git Bash / MSYS rewrites any argument that looks like a Unix path into a
# Windows path. An ARM scope like /subscriptions/<id>/resourceGroups/... comes
# out the other side as C:/Program Files/Git/subscriptions/<id>/... and Azure
# rejects it with a thoroughly misleading "(MissingSubscription) The request
# did not have a subscription or a valid tenant level resource provider."
# Exclude just those arguments: a blanket opt-out (MSYS_NO_PATHCONV=1) would
# break the --policy @<tmpfile> argument below, which does need converting.
# Unset on Linux/macOS, where it's simply ignored.
export MSYS2_ARG_CONV_EXCL='/subscriptions/'

LOCATION="${LOCATION:-northeurope}"
RG_NAME="${RG_NAME:-rg-fragvault-tfstate}"
SA_NAME="${SA_NAME:-stfragvaulttfstate}"
CONTAINER_NAME="${CONTAINER_NAME:-tfstate}"
# Service principal CI authenticates as (the AZURE_CLIENT_ID repo secret).
# Leave empty to skip that role assignment and do it separately.
CI_CLIENT_ID="${CI_CLIENT_ID:-}"
# The signed-in user also needs data-plane access to run Terraform locally —
# subscription Owner does not imply it. Set to false to skip.
GRANT_CALLER="${GRANT_CALLER:-true}"
# CI also needs control-plane rights to create the app infrastructure itself.
# Subscription scope rather than resource-group scope because Terraform
# creates the resource group too, so there's nothing narrower to scope to
# until it exists. Set to false if you'd rather grant this by hand.
#
# To tighten it later: create the app resource group by hand, change the
# Terraform config to read it as a data source instead of creating it, and
# scope Contributor to that group instead of the whole subscription. That
# trades one manual step for a much smaller blast radius if the CI
# credentials ever leak — worth doing before this holds anything real.
GRANT_CI_SUBSCRIPTION="${GRANT_CI_SUBSCRIPTION:-true}"
CI_SUBSCRIPTION_ROLE="${CI_SUBSCRIPTION_ROLE:-Contributor}"

# Storage account names are globally unique across all of Azure, lowercase
# alphanumeric, 3-24 chars. If the default is taken, re-run with SA_NAME set
# to something else and update the backend block in ../providers.tf to match.

# Azure's control plane replicates asynchronously: a resource that was just
# created reports ResourceNotFound on the role-assignment endpoint, and a
# fresh role assignment isn't honoured by the data plane for a minute or two.
# Both are transient, so anything touching a just-created scope gets retried.
# Nothing here hides stderr — a swallowed error once made this script report a
# successful role assignment that had actually failed, and CI would only have
# found out at the first `terraform init`.
retry() {
  local what="$1"
  shift
  local attempt
  for attempt in 1 2 3 4 5 6; do
    if "$@"; then
      return 0
    fi
    if [ "$attempt" -lt 6 ]; then
      echo "    $what: attempt $attempt failed, retrying in 20s (Azure propagation)"
      sleep 20
    fi
  done
  echo "    $what: giving up after 6 attempts" >&2
  return 1
}

# Role assignments are not idempotent — a second create returns
# RoleAssignmentExists with a non-zero exit, which is success as far as this
# script is concerned. Everything else gets retried: the Authorization API
# returns ResourceNotFound against freshly created scopes and intermittent
# 503s of its own. Output is captured only so it can be inspected, then
# printed in full either way.
ensure_role() {
  local object_id="$1" principal_type="$2" role="$3" scope="$4"
  local out rc attempt
  for attempt in 1 2 3 4 5 6; do
    if out="$(az role assignment create \
      --assignee-object-id "$object_id" \
      --assignee-principal-type "$principal_type" \
      --role "$role" \
      --scope "$scope" \
      --output table 2>&1)"; then
      rc=0
    else
      rc=$?
    fi
    printf '%s\n' "$out"
    [ "$rc" -eq 0 ] && return 0
    case "$out" in
      *RoleAssignmentExists*)
        echo "    already assigned: $role"
        return 0
        ;;
    esac
    if [ "$attempt" -lt 6 ]; then
      echo "    assign '$role': attempt $attempt failed, retrying in 20s"
      sleep 20
    fi
  done
  echo "    assign '$role': giving up after 6 attempts" >&2
  return 1
}

echo "==> Resource group: $RG_NAME ($LOCATION)"
az group create \
  --name "$RG_NAME" \
  --location "$LOCATION" \
  --tags project=fragvault managed_by=bootstrap-script purpose=tfstate \
  --output table

# A fresh subscription has no resource providers registered beyond
# Microsoft.Resources, and the error you get without this is a spectacularly
# misleading "(SubscriptionNotFound) Subscription <id> was not found."
# Registration is free, one-time per subscription, and takes a minute or two.
if [ "$(az provider show --namespace Microsoft.Storage --query registrationState --output tsv 2>/dev/null || true)" != "Registered" ]; then
  echo "==> Registering the Microsoft.Storage resource provider (one-time, ~1-2 min)"
  az provider register --namespace Microsoft.Storage --wait
fi

echo "==> Storage account: $SA_NAME"
# Cheapest sane configuration: Standard tier, locally-redundant (no geo
# replication to pay for), hot access tier. The state file is tens of KB, so
# this costs cents a month; hot beats cool here because cool charges more per
# transaction and has a 30-day early-deletion penalty that state churn hits.
#
# Shared key access is off from the start, not switched off at the end. Keys
# are the main leak risk on a state account, nothing here needs them, and
# leaving them on during setup would make a re-run of this script behave
# differently from the first run.
az storage account create \
  --name "$SA_NAME" \
  --resource-group "$RG_NAME" \
  --location "$LOCATION" \
  --sku Standard_LRS \
  --kind StorageV2 \
  --access-tier Hot \
  --https-only true \
  --min-tls-version TLS1_2 \
  --allow-blob-public-access false \
  --allow-shared-key-access false \
  --tags project=fragvault managed_by=bootstrap-script purpose=tfstate \
  --output table

SA_ID="$(az storage account show --name "$SA_NAME" --resource-group "$RG_NAME" --query id --output tsv)"

if [ "$GRANT_CALLER" = "true" ]; then
  echo "==> Granting Storage Blob Data Contributor to the signed-in user"
  # Needed before the container can be created below, since shared keys are
  # disabled and every data-plane call authenticates with Entra ID.
  #
  # This lookup intermittently returns "(MissingSubscription) The request did
  # not have a subscription or a valid tenant level resource provider" —
  # a transient Graph error, not a real one, so retry rather than abort.
  CALLER_OBJECT_ID=""
  for attempt in 1 2 3; do
    CALLER_OBJECT_ID="$(az ad signed-in-user show --query id --output tsv || true)"
    [ -n "$CALLER_OBJECT_ID" ] && break
    echo "    signed-in user lookup failed (attempt $attempt), retrying in 10s"
    sleep 10
  done
  if [ -n "$CALLER_OBJECT_ID" ]; then
    ensure_role "$CALLER_OBJECT_ID" User "Storage Blob Data Contributor" "$SA_ID"
  else
    # Not fatal: CI doesn't need this, only local Terraform runs do.
    echo "    could not resolve the signed-in user — skipping." >&2
    echo "    Local 'terraform plan' will 403 on the state blob until this" >&2
    echo "    account has Storage Blob Data Contributor on $SA_NAME." >&2
  fi
fi

echo "==> Blob versioning + soft delete"
# Both are effectively free at this size and are the difference between "oops"
# and "restore yesterday's state". Old versions are pruned after 30 days by
# the lifecycle rule below so they can't accumulate cost.
az storage account blob-service-properties update \
  --account-name "$SA_NAME" \
  --resource-group "$RG_NAME" \
  --enable-versioning true \
  --enable-delete-retention true \
  --delete-retention-days 7 \
  --enable-container-delete-retention true \
  --container-delete-retention-days 7 \
  --output table

echo "==> Container: $CONTAINER_NAME"
# --auth-mode login because shared keys are disabled. Retried because the role
# assignment above takes a minute or two to reach the data plane; until it
# does, this returns AuthorizationPermissionMismatch.
retry "create container" az storage container create \
  --name "$CONTAINER_NAME" \
  --account-name "$SA_NAME" \
  --auth-mode login \
  --output table

echo "==> Lifecycle rule: expire state versions older than 30 days"
POLICY_FILE="$(mktemp)"
trap 'rm -f "$POLICY_FILE"' EXIT
cat > "$POLICY_FILE" <<'JSON'
{
  "rules": [
    {
      "enabled": true,
      "name": "expire-old-state-versions",
      "type": "Lifecycle",
      "definition": {
        "filters": { "blobTypes": ["blockBlob"] },
        "actions": {
          "version": { "delete": { "daysAfterCreationGreaterThan": 30 } }
        }
      }
    }
  ]
}
JSON
az storage account management-policy create \
  --account-name "$SA_NAME" \
  --resource-group "$RG_NAME" \
  --policy "@$POLICY_FILE" \
  --output table

if [ -n "$CI_CLIENT_ID" ]; then
  echo "==> Granting Storage Blob Data Contributor to CI service principal"
  # The backend authenticates with Entra ID (use_azuread_auth in
  # providers.tf), so no storage access key ever needs to become a GitHub
  # secret. Subscription Contributor does NOT imply data-plane access —
  # this role assignment is what actually lets CI read/write the state blob.
  SP_OBJECT_ID="$(az ad sp show --id "$CI_CLIENT_ID" --query id --output tsv)"
  ensure_role "$SP_OBJECT_ID" ServicePrincipal "Storage Blob Data Contributor" "$SA_ID"

  if [ "$GRANT_CI_SUBSCRIPTION" = "true" ]; then
    echo "==> Granting $CI_SUBSCRIPTION_ROLE on the subscription to CI"
    # Data-plane access to the state blob is not enough — CI also has to
    # create the resource group, VM, network, and everything else. That is a
    # control-plane role, and it has to sit at subscription scope because
    # Terraform creates the resource group itself; there is no narrower
    # existing scope to attach it to.
    SUBSCRIPTION_ID="$(az account show --query id --output tsv)"
    ensure_role "$SP_OBJECT_ID" ServicePrincipal "$CI_SUBSCRIPTION_ROLE" \
      "/subscriptions/$SUBSCRIPTION_ID"
  else
    echo "==> GRANT_CI_SUBSCRIPTION=false — skipping the subscription role."
    echo "    CI can read/write state but cannot create infrastructure."
  fi
else
  echo "==> CI_CLIENT_ID not set — skipping the CI role assignments."
  echo "    CI cannot read state until its service principal gets"
  echo "    'Storage Blob Data Contributor' on $SA_NAME, nor create"
  echo "    infrastructure without a control-plane role on the subscription."
fi

echo
echo "Done. Backend config already in ../providers.tf:"
echo "  resource_group_name  = \"$RG_NAME\""
echo "  storage_account_name = \"$SA_NAME\""
echo "  container_name       = \"$CONTAINER_NAME\""
echo
echo "If you changed any of the names above, update providers.tf to match,"
echo "then run 'terraform init' in ../ to migrate local state to the backend."
