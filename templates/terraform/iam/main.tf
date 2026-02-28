terraform {
  required_version = ">= 1.7.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

# ─── GitHub Actions OIDC Provider ────────────────────────────────────────────

resource "aws_iam_openid_connect_provider" "github_actions" {
  url            = "https://token.actions.githubusercontent.com"
  client_id_list = ["sts.amazonaws.com"]

  # GitHub's OIDC thumbprints — these are static for GitHub's TLS cert chain.
  thumbprint_list = [
    "6938fd4d98bab03faadb97b34396831e3780aea1",
    "1c58a3a8518e8759bf075b76b750d4f2df264fcd",
  ]

  tags = {
    Name      = "GitHub Actions OIDC"
    ManagedBy = "Scaffold"
  }
}

# ─── IAM Role ────────────────────────────────────────────────────────────────

resource "aws_iam_role" "github_actions" {
  name               = "github-actions-${var.environment_name}"
  assume_role_policy = data.aws_iam_policy_document.github_actions_assume_role.json

  tags = {
    Name        = "GitHub Actions Role - ${var.environment_name}"
    ManagedBy   = "Scaffold"
    Environment = var.environment_name
  }
}

# Trust policy: accept both branch refs AND GitHub environment sub claims.
# This is critical — GitHub sends different sub claims depending on whether
# the job uses an environment or not.
data "aws_iam_policy_document" "github_actions_assume_role" {
  statement {
    effect = "Allow"

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github_actions.arn]
    }

    actions = ["sts:AssumeRoleWithWebIdentity"]

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    # Accept both ref-based and environment-based sub claims
    condition {
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      values = [
        "repo:${var.github_org}/${var.github_repo}:ref:refs/heads/*",
        "repo:${var.github_org}/${var.github_repo}:environment:*",
      ]
    }
  }
}

# ─── Inline Policy: PowerUser (SCP-compliant) ────────────────────────────────
# Use aws_iam_role_policy (inline) rather than aws_iam_role_policy_attachment
# to avoid SCP restrictions that block managed policy attachments.

resource "aws_iam_role_policy" "power_user_permissions" {
  name   = "power-user-permissions"
  role   = aws_iam_role.github_actions.name
  policy = data.aws_iam_policy_document.power_user_inline.json
}

data "aws_iam_policy_document" "power_user_inline" {
  # Allow everything except sensitive admin actions
  statement {
    sid       = "PowerUserAccess"
    effect    = "Allow"
    actions   = ["*"]
    resources = ["*"]
  }

  # Deny IAM/Orgs write actions (least privilege — Terraform shouldn't need these)
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

  # Re-allow IAM read actions (Terraform needs these for data sources)
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

# ─── Inline Policy: Cross-account backend access ─────────────────────────────

resource "aws_iam_role_policy" "backend_access" {
  name   = "terraform-backend-access"
  role   = aws_iam_role.github_actions.name
  policy = data.aws_iam_policy_document.backend_access.json
}

data "aws_iam_policy_document" "backend_access" {
  statement {
    sid    = "S3StateAccess"
    effect = "Allow"
    actions = [
      "s3:ListBucket",
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
    ]
    resources = [
      "arn:aws:s3:::${var.backend_s3_bucket}",
      "arn:aws:s3:::${var.backend_s3_bucket}/${var.environment_name}/*",
    ]
  }

  statement {
    sid    = "DynamoDBLockAccess"
    effect = "Allow"
    actions = [
      "dynamodb:PutItem",
      "dynamodb:GetItem",
      "dynamodb:DeleteItem",
      "dynamodb:DescribeTable",
    ]
    resources = [
      "arn:aws:dynamodb:${var.backend_region}:${var.backend_account_id}:table/${var.backend_dynamodb_table}",
    ]
  }

  statement {
    sid    = "KMSKeyAccess"
    effect = "Allow"
    actions = [
      "kms:Decrypt",
      "kms:Encrypt",
      "kms:GenerateDataKey",
      "kms:DescribeKey",
    ]
    resources = [var.backend_kms_key_arn]
  }

  statement {
    sid       = "GetCallerIdentity"
    effect    = "Allow"
    actions   = ["sts:GetCallerIdentity"]
    resources = ["*"]
  }
}
