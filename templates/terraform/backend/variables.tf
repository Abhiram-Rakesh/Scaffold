# =============================================================================
# backend/variables.tf - Variables for Backend Resources
# =============================================================================

variable "aws_region" {
  description = "AWS region where resources will be created"
  type        = string
}

variable "bucket_name" {
  description = "S3 bucket name for storing Terraform state files"
  type        = string
}

variable "dynamodb_table_name" {
  description = "DynamoDB table name for Terraform state locking"
  type        = string
}
