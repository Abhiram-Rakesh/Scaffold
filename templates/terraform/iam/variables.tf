variable "environment_name" {
  description = "Environment name (e.g. staging, production)"
  type        = string
}

variable "github_org" {
  description = "GitHub organization name"
  type        = string
}

variable "github_repo" {
  description = "GitHub repository name"
  type        = string
}

variable "backend_s3_bucket" {
  description = "Name of the centralized S3 state bucket"
  type        = string
}

variable "backend_dynamodb_table" {
  description = "Name of the DynamoDB lock table"
  type        = string
}

variable "backend_kms_key_arn" {
  description = "ARN of the KMS key used for state encryption"
  type        = string
}

variable "backend_region" {
  description = "AWS region of the state backend"
  type        = string
}

variable "backend_account_id" {
  description = "AWS account ID hosting the state backend"
  type        = string
}
