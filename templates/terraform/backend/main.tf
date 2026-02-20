# =============================================================================
# backend/main.tf - Terraform Backend Resources
# =============================================================================
# This module creates the shared infrastructure for storing Terraform state:
#   - S3 bucket with versioning, encryption, and lifecycle policies
#   - DynamoDB table for state locking (prevents concurrent modifications)
# =============================================================================

terraform {
  required_version = ">= 1.7.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

# Configure AWS provider with the specified region
provider "aws" {
  region = var.aws_region
}

# =============================================================================
# S3 Bucket for Terraform State Storage
# =============================================================================
# This bucket stores all Terraform state files securely with:
#   - Versioning enabled (preserves state history)
#   - Server-side encryption (AES256)
#   - Public access blocked
#   - Lifecycle rules for cost optimization

resource "aws_s3_bucket" "terraform_state" {
  bucket        = var.bucket_name
  force_destroy = false # Prevent accidental deletion of state

  tags = {
    ManagedBy = "scaffold"
    Purpose   = "terraform-state"
  }
}

# Enable versioning to preserve state file history
# Allows rollback if state becomes corrupted
resource "aws_s3_bucket_versioning" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id
  versioning_configuration {
    status = "Enabled"
  }
}

# Enable server-side encryption with AES-256
# All state files are encrypted at rest
resource "aws_s3_bucket_server_side_encryption_configuration" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Block all public access to the state bucket
# Terraform state may contain sensitive information
resource "aws_s3_bucket_public_access_block" "terraform_state" {
  bucket                  = aws_s3_bucket.terraform_state.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Lifecycle configuration to manage storage costs
#   - Transition old versions to STANDARD_IA after 30 days
#   - Delete old versions after 90 days
resource "aws_s3_bucket_lifecycle_configuration" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id

  rule {
    id     = "expire-old-versions"
    status = "Enabled"

    # CRITICAL: prefix filter required to avoid deprecated "filter {}" warning
    filter {
      prefix = ""
    }

    # Delete non-current versions after 90 days
    noncurrent_version_expiration {
      noncurrent_days = 90
    }

    # Transition to Standard-IA after 30 days for cost savings
    noncurrent_version_transition {
      noncurrent_days = 30
      storage_class   = "STANDARD_IA"
    }
  }
}

# =============================================================================
# DynamoDB Table for State Locking
# =============================================================================
# This table prevents concurrent Terraform operations from corrupting state.
# When one user/process is running terraform, a lock record is created.
# Other operations must wait until the lock is released.

resource "aws_dynamodb_table" "terraform_locks" {
  name         = var.dynamodb_table_name
  billing_mode = "PAY_PER_REQUEST" # Pay only for what you use
  hash_key     = "LockID"          # Partition key for lock records

  # Define the LockID attribute (string type)
  attribute {
    name = "LockID"
    type = "S"
  }

  tags = {
    ManagedBy = "scaffold"
    Purpose   = "terraform-state-lock"
  }
}
