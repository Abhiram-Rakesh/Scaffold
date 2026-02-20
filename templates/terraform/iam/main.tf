terraform {
  required_version = ">= 1.7.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# ─── OIDC Provider (idempotent) ───────────────────────────────────────────────

data "aws_caller_identity" "current" {}

# GitHub's OIDC thumbprint (stable)
locals {
  github_oidc_url       = "https://token.actions.githubusercontent.com"
  github_oidc_thumbprint = "6938fd4d98bab03faadb97b34396831e3780aea1"
}

resource "aws_iam_openid_connect_provider" "github" {
  url             = local.github_oidc_url
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = [local.github_oidc_thumbprint]

  tags = {
    ManagedBy = "scaffold"
  }

  # If provider already exists, import it rather than failing
  lifecycle {
    ignore_changes = [thumbprint_list]
  }
}

# ─── IAM Role ─────────────────────────────────────────────────────────────────

data "aws_iam_policy_document" "github_assume_role" {
  statement {
    sid     = "GitHubActionsOIDC"
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }

    condition {
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      # Accept both branch refs and environment-based sub claims
      values = [
        "repo:${var.github_org}/${var.github_repo}:ref:refs/heads/*",
        "repo:${var.github_org}/${var.github_repo}:environment:*",
      ]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "github_actions" {
  name               = var.role_name
  assume_role_policy = data.aws_iam_policy_document.github_assume_role.json
  max_session_duration = 3600

  tags = {
    ManagedBy  = "scaffold"
    GitHubRepo = "${var.github_org}/${var.github_repo}"
  }
}

# ─── Inline Policies (SCP-Compliant) ─────────────────────────────────────────

data "aws_iam_policy_document" "power_user_inline" {
  statement {
    sid       = "PowerUserAccess"
    effect    = "Allow"
    actions   = ["*"]
    resources = ["*"]
  }

  statement {
    sid    = "DenyIAMAndOrgs"
    effect = "Deny"
    actions = [
      "iam:*",
      "organizations:*",
      "account:*",
    ]
    resources = ["*"]
  }

  # Allow read-only IAM so Terraform can introspect roles/policies it manages
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

# Inline policy - not a managed policy attachment, bypasses SCP iam:AttachRolePolicy deny
resource "aws_iam_role_policy" "power_user_permissions" {
  count  = var.use_inline_policies ? 1 : 0
  name   = "power-user-permissions"
  role   = aws_iam_role.github_actions.name
  policy = data.aws_iam_policy_document.power_user_inline.json
}

# ─── State Backend Access Policy ─────────────────────────────────────────────

data "aws_iam_policy_document" "state_backend_access" {
  statement {
    sid    = "S3StateAccess"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
      "s3:ListBucket",
      "s3:GetBucketVersioning",
    ]
    resources = [
      "arn:aws:s3:::${var.s3_bucket}",
      "arn:aws:s3:::${var.s3_bucket}/*",
    ]
  }

  statement {
    sid    = "DynamoDBLockAccess"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:DeleteItem",
      "dynamodb:DescribeTable",
    ]
    resources = [
      "arn:aws:dynamodb:${var.aws_region}:${data.aws_caller_identity.current.account_id}:table/${var.dynamodb_table}",
    ]
  }

  statement {
    sid       = "STSCallerIdentity"
    effect    = "Allow"
    actions   = ["sts:GetCallerIdentity"]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "state_backend_access" {
  name   = "state-backend-access"
  role   = aws_iam_role.github_actions.name
  policy = data.aws_iam_policy_document.state_backend_access.json
}

# ─── Outputs ──────────────────────────────────────────────────────────────────

output "role_arn" {
  description = "ARN of the IAM role for GitHub Actions"
  value       = aws_iam_role.github_actions.arn
}

output "oidc_provider_arn" {
  description = "ARN of the GitHub OIDC provider"
  value       = aws_iam_openid_connect_provider.github.arn
}
