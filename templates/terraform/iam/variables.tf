# =============================================================================
# iam/variables.tf - Variables for IAM Module
# =============================================================================

variable "aws_region" {
  description = "AWS region where resources are located"
  type        = string
}

variable "role_name" {
  description = "Name of the IAM role that GitHub Actions will assume"
  type        = string
}

variable "github_org" {
  description = "GitHub organization or username (owner of the repository)"
  type        = string
}

variable "github_repo" {
  description = "GitHub repository name"
  type        = string
}

variable "s3_bucket" {
  description = "S3 bucket name for storing Terraform state"
  type        = string
}

variable "dynamodb_table" {
  description = "DynamoDB table name for Terraform state locking"
  type        = string
}

variable "use_inline_policies" {
  description = "Use inline IAM policies instead of managed policy attachments (required for SCP compliance)"
  type        = bool
  default     = true
}
