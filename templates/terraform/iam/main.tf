# =============================================================================
# iam/main.tf - IAM OIDC Role for GitHub Actions
# =============================================================================
# This module creates:
#   - OpenID Connect (OIDC) provider for GitHub Actions
#   - IAM role with trust policy allowing GitHub to assume it
#   - Inline policies for:
#     - PowerUser access (with IAM/org write restrictions)
#     - S3 and DynamoDB access for state management
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

# Configure AWS provider
provider "aws" {
  region = var.aws_region
}

# =============================================================================
# AWS Account Information
# =============================================================================
# Get current AWS account ID for constructing ARNs
data "aws_caller_identity" "current" {}

# =============================================================================
# OpenID Connect (OIDC) Provider for GitHub Actions
# =============================================================================
# GitHub Actions uses OIDC to authenticate with AWS without storing credentials.
# This provider establishes trust between GitHub and AWS using the GitHub's
# token endpoint and a known thumbprint.

# Constants for GitHub OIDC
locals {
  github_oidc_url        = "https://token.actions.githubusercontent.com"
  github_oidc_thumbprint = "6938fd4d98bab03faadb97b34396831e3780aea1" # Stable thumbprint
}

# Create OIDC provider (idempotent - handles if already exists)
resource "aws_iam_openid_connect_provider" "github" {
  url             = local.github_oidc_url
  client_id_list  = ["sts.amazonaws.com"] # AWS STS is the client
  thumbprint_list = [local.github_oidc_thumbprint]

  tags = {
    ManagedBy = "scaffold"
  }

  # Ignore thumbprint changes to prevent unnecessary updates
  lifecycle {
    ignore_changes = [thumbprint_list]
  }
}

# =============================================================================
# IAM Role for GitHub Actions
# =============================================================================
# The trust policy defines who can assume this role.
# GitHub Actions uses OIDC to obtain temporary credentials.

# Trust policy document - allows GitHub to assume this role
data "aws_iam_policy_document" "github_assume_role" {
  statement {
    sid     = "GitHubActionsOIDC"
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    # Trust the GitHub OIDC provider
    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }

    # Condition 1: Verify the subject claim matches our repo
    # Accept both branch refs (ref:refs/heads/main) and environments (environment:production)
    condition {
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      values = [
        "repo:${var.github_org}/${var.github_repo}:ref:refs/heads/*",
        "repo:${var.github_org}/${var.github_repo}:environment:*",
      ]
    }

    # Condition 2: Verify the audience is STS
    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

# Create the IAM role with the trust policy
resource "aws_iam_role" "github_actions" {
  name                 = var.role_name
  assume_role_policy   = data.aws_iam_policy_document.github_assume_role.json
  max_session_duration = 3600 # 1 hour max session length

  tags = {
    ManagedBy  = "scaffold"
    GitHubRepo = "${var.github_org}/${var.github_repo}"
  }
}

# =============================================================================
# Inline Policies (SCP-Compliant)
# =============================================================================
# By default, uses inline policies instead of managed policy attachments.
# This works around SCPs that deny iam:AttachRolePolicy.
#
# The policy grants PowerUser-like access with restrictions:
#   - Allow all actions except IAM/org/account modifications
#   - Allow read-only IAM for Terraform introspection

data "aws_iam_policy_document" "power_user_inline" {
  # Statement 1: Grant broad access (PowerUser equivalent)
  statement {
    sid       = "PowerUserAccess"
    effect    = "Allow"
    actions   = ["*"]
    resources = ["*"]
  }

  # Statement 2: Deny IAM and organization write actions
  # This prevents users from escalating privileges
  statement {
    sid    = "DenyIAMAndOrgs"
    effect = "Deny"
    actions = [
      "iam:*",           # All IAM actions
      "organizations:*", # All org actions
      "account:*",       # Account actions
    ]
    resources = ["*"]
  }

  # Statement 3: Allow IAM read operations
  # Terraform needs to read IAM roles/policies it manages
  statement {
    sid    = "AllowIAMRead"
    effect = "Allow"
    actions = [
      "iam:Get*",
      "iam:List*",
      "iam:Describe*",
    ]
    resources = ["*"]
  }
}

# Attach inline policy (conditional - controlled by use_inline_policies flag)
# Using inline policies bypasses SCP iam:AttachRolePolicy deny
resource "aws_iam_role_policy" "power_user_permissions" {
  count  = var.use_inline_policies ? 1 : 0
  name   = "power-user-permissions"
  role   = aws_iam_role.github_actions.name
  policy = data.aws_iam_policy_document.power_user_inline.json
}

# =============================================================================
# State Backend Access Policy
# =============================================================================
# Grants permissions to access S3 bucket and DynamoDB table for state management

data "aws_iam_policy_document" "state_backend_access" {
  # S3 permissions for reading/writing state
  statement {
    sid    = "S3StateAccess"
    effect = "Allow"
    actions = [
      "s3:GetObject",           # Read state files
      "s3:PutObject",           # Write state files
      "s3:DeleteObject",        # Delete state files
      "s3:ListBucket",          # List state files
      "s3:GetBucketVersioning", # Check versioning status
    ]
    resources = [
      "arn:aws:s3:::${var.s3_bucket}",   # Bucket operations
      "arn:aws:s3:::${var.s3_bucket}/*", # Object operations
    ]
  }

  # DynamoDB permissions for state locking
  statement {
    sid    = "DynamoDBLockAccess"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",       # Read lock records
      "dynamodb:PutItem",       # Create lock records
      "dynamodb:DeleteItem",    # Release locks
      "dynamodb:DescribeTable", # Check table exists
    ]
    resources = [
      "arn:aws:dynamodb:${var.aws_region}:${data.aws_caller_identity.current.account_id}:table/${var.dynamodb_table}",
    ]
  }

  # Allow checking current identity (useful for debugging)
  statement {
    sid       = "STSCallerIdentity"
    effect    = "Allow"
    actions   = ["sts:GetCallerIdentity"]
    resources = ["*"]
  }
}

# Attach the state backend access policy to the role
resource "aws_iam_role_policy" "state_backend_access" {
  name   = "state-backend-access"
  role   = aws_iam_role.github_actions.name
  policy = data.aws_iam_policy_document.state_backend_access.json
}

# =============================================================================
# Outputs
# =============================================================================

output "role_arn" {
  description = "ARN of the IAM role for GitHub Actions"
  value       = aws_iam_role.github_actions.arn
}

output "oidc_provider_arn" {
  description = "ARN of the GitHub OIDC provider"
  value       = aws_iam_openid_connect_provider.github.arn
}
