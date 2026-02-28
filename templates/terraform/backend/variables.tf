variable "backend_account_id" {
  description = "AWS account ID hosting the state backend"
  type        = string
}

variable "repo_name" {
  description = "GitHub repository name (used for resource naming)"
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

variable "aws_region" {
  description = "AWS region for backend resources"
  type        = string
  default     = "us-east-1"
}
