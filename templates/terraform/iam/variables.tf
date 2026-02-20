variable "aws_region" {
  description = "AWS region"
  type        = string
}

variable "role_name" {
  description = "Name of the IAM role for GitHub Actions"
  type        = string
}

variable "github_org" {
  description = "GitHub organization or username"
  type        = string
}

variable "github_repo" {
  description = "GitHub repository name"
  type        = string
}

variable "s3_bucket" {
  description = "S3 bucket name for Terraform state"
  type        = string
}

variable "dynamodb_table" {
  description = "DynamoDB table name for state locking"
  type        = string
}

variable "use_inline_policies" {
  description = "Use inline policies instead of managed policy attachments (SCP-compliant)"
  type        = bool
  default     = true
}
