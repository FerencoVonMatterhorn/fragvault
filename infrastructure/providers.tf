terraform {
  required_version = ">= 1.7.0"

  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.117"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }

  # Remote state lives in a storage account created outside Terraform by
  # bootstrap/bootstrap-tfstate.sh — see that script for why. State locking
  # comes free with this backend (blob lease), so no lock table to pay for.
  #
  # use_azuread_auth means the backend authenticates with the same service
  # principal as the provider (ARM_CLIENT_ID/SECRET/TENANT_ID), so no storage
  # access key needs to exist as a GitHub secret. The principal needs the
  # "Storage Blob Data Contributor" role on the account.
  backend "azurerm" {
    resource_group_name  = "rg-fragvault-tfstate"
    storage_account_name = "stfragvaulttfstate"
    container_name       = "tfstate"
    key                  = "poc.terraform.tfstate"
    use_azuread_auth     = true
  }
}

provider "azurerm" {
  features {}
}
