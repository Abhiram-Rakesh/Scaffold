# =============================================================================
# providers.tf - Terraform Provider and Backend Configuration
# =============================================================================
# This file configures:
#   - Required Terraform and provider versions
#   - AWS provider configuration
#   - S3 backend for remote state storage
#
# IMPORTANT: The backend block is empty because configuration is supplied
# via -backend-config flags when running terraform init. This is necessary
# because the actual bucket/table names are determined at init time.
# =============================================================================

terraform {
  required_version = ">= 1.7.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  # Empty backend block - configuration supplied at init time via -backend-config flags.
  # WITHOUT this block Terraform silently falls back to local state even when
  # -backend-config flags are passed on the command line.
  backend "s3" {}
}

# Configure the AWS provider with the specified region
provider "aws" {
  region = var.aws_region
}

# Variable for AWS region (default can be overridden at runtime)
variable "aws_region" {
  description = "AWS region for resources"
  type        = string
  default     = "us-east-1" # overridden by scaffold init
}
