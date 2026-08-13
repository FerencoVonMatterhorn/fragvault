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

  # Remote state: configure a backend once the storage account for it exists
  # (chicken/egg — bootstrap that by hand once, then wire it in here).
  # backend "azurerm" {}
}

provider "azurerm" {
  features {}
}
