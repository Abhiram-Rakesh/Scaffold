variable "aws_region" {
  description = "AWS region for resources"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project tag/name used for generated resources"
  type        = string
  default     = "scaffold-pipeline-test"
}
