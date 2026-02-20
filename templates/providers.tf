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

provider "aws" {
  region = var.aws_region
}

variable "aws_region" {
  description = "AWS region for resources"
  type        = string
  default     = "us-east-1" # overridden by scaffold init
}
