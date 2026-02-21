#!/usr/bin/env bash
# =============================================================================
# init.sh - scaffold init command
# =============================================================================
# Provisions AWS resources and generates GitHub Actions workflows.
# This is the main bootstrapping command that sets up the entire CI/CD pipeline.
#
# What it creates:
#   - S3 bucket for Terraform state storage
#   - DynamoDB table for state locking
#   - IAM OIDC role for GitHub Actions authentication
#   - GitHub Actions workflow files
#   - Terraform providers.tf configuration
#   - Scaffold configuration file
# =============================================================================

set -euo pipefail

SCAFFOLD_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/common.sh
source "$SCAFFOLD_ROOT/lib/common.sh"

# =============================================================================
# Command-line Flags
# =============================================================================
USE_INLINE_POLICIES=true   # Use inline IAM policies (SCP-compliant by default)
SHARED_ROLE=false          # Create one IAM role shared across all environments

# Parse command-line arguments
for arg in "$@"; do
  case "$arg" in
    --use-inline-policies=false) USE_INLINE_POLICIES=false ;;  # Use managed policies instead
    --shared-role)               SHARED_ROLE=true ;;            # Single role for all envs
  esac
done

# =============================================================================
# S3 Bucket Creation
# =============================================================================
# Creates or imports an S3 bucket for storing Terraform state
# If the bucket already exists, imports it into Terraform state
create_s3_bucket() {
  local bucket="$1"

  if s3_bucket_exists "$bucket"; then
    # Bucket exists - import into Terraform state for management
    warn "S3 bucket $bucket already exists — importing."
    terraform -chdir="$TF_BACKEND_DIR" import aws_s3_bucket.terraform_state "$bucket" 2>/dev/null || true
  else
    # Create new bucket with versioning, encryption, and lifecycle policies
    terraform -chdir="$TF_BACKEND_DIR" apply -auto-approve \
      -var="bucket_name=$bucket" \
      -var="dynamodb_table_name=$DYNAMO_TABLE" \
      -var="aws_region=$AWS_REGION" \
      -target=aws_s3_bucket.terraform_state \
      -target=aws_s3_bucket_versioning.terraform_state \
      -target=aws_s3_bucket_server_side_encryption_configuration.terraform_state \
      -target=aws_s3_bucket_public_access_block.terraform_state \
      -target=aws_s3_bucket_lifecycle_configuration.terraform_state
  fi
}

# =============================================================================
# DynamoDB Table Creation
# =============================================================================
# Creates or imports a DynamoDB table for Terraform state locking
# Prevents concurrent Terraform operations from corrupting state
create_dynamodb_table() {
  local table="$1"

  if dynamo_table_exists "$table"; then
    # Table exists - import into Terraform state
    warn "DynamoDB table $table already exists — importing."
    terraform -chdir="$TF_BACKEND_DIR" import aws_dynamodb_table.terraform_locks "$table" 2>/dev/null || true
  else
    # Create new table with LockID as the hash key
    terraform -chdir="$TF_BACKEND_DIR" apply -auto-approve \
      -var="bucket_name=$S3_BUCKET" \
      -var="dynamodb_table_name=$table" \
      -var="aws_region=$AWS_REGION" \
      -target=aws_dynamodb_table.terraform_locks
  fi
}

# =============================================================================
# IAM OIDC Role Creation
# =============================================================================
# Creates an IAM role that GitHub Actions can assume via OIDC
# This enables passwordless authentication without storing AWS credentials
create_iam_role() {
  local role_name="$1"
  local env_name="$2"

  if iam_role_exists "$role_name"; then
    # Role exists - import into Terraform state
    warn "IAM role $role_name already exists — importing."
    terraform -chdir="$TF_IAM_DIR" import aws_iam_role.github_actions "$role_name" 2>/dev/null || true
  fi

  # Apply the IAM module to create/update the role with appropriate policies
  terraform -chdir="$TF_IAM_DIR" apply -auto-approve \
    -var="role_name=$role_name" \
    -var="github_org=$GITHUB_ORG" \
    -var="github_repo=$GITHUB_REPO" \
    -var="s3_bucket=$(s3_bucket_name)" \
    -var="dynamodb_table=$(dynamo_table_name)" \
    -var="aws_region=$AWS_REGION" \
    -var="use_inline_policies=$USE_INLINE_POLICIES"
}

# =============================================================================
# GitHub Actions Workflow Generation
# =============================================================================
# Generates a GitHub Actions workflow file from the template
# The workflow handles Terraform plan and apply on push events
generate_workflow() {
  local env_name="$1" watch_dir="$2" branch="$3" role_arn="$4"
  local workflow_dir=".github/workflows"
  local workflow_file="$workflow_dir/terraform-${env_name}.yaml"

  mkdir -p "$workflow_dir"

  local s3_bucket; s3_bucket=$(s3_bucket_name)
  local dynamo_table; dynamo_table=$(dynamo_table_name)

  # Replace template placeholders with actual values
  sed \
    -e "s|{{ENV_NAME}}|$env_name|g" \
    -e "s|{{WATCH_DIR}}|$watch_dir|g" \
    -e "s|{{BRANCH}}|$branch|g" \
    -e "s|{{ROLE_ARN}}|$role_arn|g" \
    -e "s|{{S3_BUCKET}}|$s3_bucket|g" \
    -e "s|{{DYNAMO_TABLE}}|$dynamo_table|g" \
    -e "s|{{AWS_REGION}}|$AWS_REGION|g" \
    -e "s|{{STATE_KEY}}|${env_name}/terraform.tfstate|g" \
    "$SCAFFOLD_ROOT/templates/workflow.yaml" > "$workflow_file"

  ok "Workflow: $workflow_file"
}

# =============================================================================
# Terraform providers.tf Generation
# =============================================================================
# Creates the providers.tf file in the environment's watch directory
# This file configures Terraform to use S3 backend with the created resources
generate_providers_tf() {
  local watch_dir="$1"
  mkdir -p "$watch_dir"
  local providers_file="$watch_dir/providers.tf"

  # Skip if already exists to avoid overwriting user changes
  if [[ -f "$providers_file" ]]; then
    warn "$providers_file already exists — skipping."
    return
  fi

  # Create providers.tf with S3 backend configuration
  # The backend block is empty - config is supplied via -backend-config flags
  # Note: Provider config should be defined in user's own terraform files
  cat > "$providers_file" <<EOF
terraform {
  required_version = ">= 1.7.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
  backend "s3" {}
}
EOF
  ok "providers.tf: $providers_file"
}

# ─── DynamoDB Table ───────────────────────────────────────────────────────────
create_dynamodb_table() {
  local table="$1"

  if dynamo_table_exists "$table"; then
    warn "DynamoDB table $table already exists — importing."
    terraform -chdir="$TF_BACKEND_DIR" import aws_dynamodb_table.terraform_locks "$table" 2>/dev/null || true
  else
    terraform -chdir="$TF_BACKEND_DIR" apply -auto-approve \
      -var="bucket_name=$S3_BUCKET" \
      -var="dynamodb_table_name=$table" \
      -var="aws_region=$AWS_REGION" \
      -target=aws_dynamodb_table.terraform_locks
  fi
}

# ─── IAM OIDC Role ────────────────────────────────────────────────────────────
create_iam_role() {
  local role_name="$1"
  local env_name="$2"

  if iam_role_exists "$role_name"; then
    warn "IAM role $role_name already exists — importing."
    terraform -chdir="$TF_IAM_DIR" import aws_iam_role.github_actions "$role_name" 2>/dev/null || true
  fi

  terraform -chdir="$TF_IAM_DIR" apply -auto-approve \
    -var="role_name=$role_name" \
    -var="github_org=$GITHUB_ORG" \
    -var="github_repo=$GITHUB_REPO" \
    -var="s3_bucket=$(s3_bucket_name)" \
    -var="dynamodb_table=$(dynamo_table_name)" \
    -var="aws_region=$AWS_REGION" \
    -var="use_inline_policies=$USE_INLINE_POLICIES"
}

# ─── GitHub Actions Workflow ──────────────────────────────────────────────────
generate_workflow() {
  local env_name="$1" watch_dir="$2" branch="$3" role_arn="$4"
  local workflow_dir=".github/workflows"
  local workflow_file="$workflow_dir/terraform-${env_name}.yaml"

  mkdir -p "$workflow_dir"

  local s3_bucket; s3_bucket=$(s3_bucket_name)
  local dynamo_table; dynamo_table=$(dynamo_table_name)

  sed \
    -e "s|{{ENV_NAME}}|$env_name|g" \
    -e "s|{{WATCH_DIR}}|$watch_dir|g" \
    -e "s|{{BRANCH}}|$branch|g" \
    -e "s|{{ROLE_ARN}}|$role_arn|g" \
    -e "s|{{S3_BUCKET}}|$s3_bucket|g" \
    -e "s|{{DYNAMO_TABLE}}|$dynamo_table|g" \
    -e "s|{{AWS_REGION}}|$AWS_REGION|g" \
    -e "s|{{STATE_KEY}}|${env_name}/terraform.tfstate|g" \
    "$SCAFFOLD_ROOT/templates/workflow.yaml" > "$workflow_file"

  ok "Workflow: $workflow_file"
}

# ─── providers.tf ─────────────────────────────────────────────────────────────
generate_providers_tf() {
  local watch_dir="$1"
  mkdir -p "$watch_dir"
  local providers_file="$watch_dir/providers.tf"

  if [[ -f "$providers_file" ]]; then
    warn "$providers_file already exists — skipping."
    return
  fi

  cat > "$providers_file" <<EOF
terraform {
  required_version = ">= 1.7.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
  backend "s3" {}
}
EOF
  ok "providers.tf: $providers_file"
}

# =============================================================================
# Main Entry Point
# =============================================================================
# Interactive wizard that prompts for configuration and provisions all resources
main() {
  # Display banner and detect repository from git remote
  banner
  detect_repo
  echo "  Auto-detected repository: ${GITHUB_ORG}/${GITHUB_REPO}"
  echo ""

  # Configure AWS credentials
  configure_aws

  # ===========================================================================
  # Step 1: Terraform Configuration
  # ===========================================================================
  header "Terraform Configuration"
  echo ""
  
  # Ask for AWS region (defaults to environment or us-east-1)
  read -rp "  Region [$( [[ -n "${AWS_DEFAULT_REGION:-}" ]] && echo "${AWS_DEFAULT_REGION}" || echo "us-east-1")]: " AWS_REGION
  AWS_REGION="${AWS_REGION:-${AWS_DEFAULT_REGION:-us-east-1}}"
  export AWS_REGION

  # Ask for number of environments (e.g., staging, production)
  echo ""
  read -rp "  How many environments? [1]: " num_envs
  num_envs="${num_envs:-1}"

  # Collect environment details (name, directory, branch)
  declare -a ENV_NAMES ENV_DIRS ENV_BRANCHES
  for (( i=1; i<=num_envs; i++ )); do
    echo ""
    echo "  Environment $i:"
    read -rp "    Name: " env_name
    read -rp "    Watch directory: " watch_dir
    read -rp "    Trigger branch [main]: " trigger_branch
    trigger_branch="${trigger_branch:-main}"
    ENV_NAMES+=("$env_name")
    ENV_DIRS+=("$watch_dir")
    ENV_BRANCHES+=("$trigger_branch")
  done

  # ===========================================================================
  # Step 2: IAM Policy Mode
  # ===========================================================================
  # Ask about inline vs managed policies (for SCP compliance)
  echo ""
  header "IAM Policy Mode"
  read -rp "  Use inline policies (SCP-compliant)? [Y/n]: " inline_choice
  inline_choice="${inline_choice:-y}"
  [[ "$inline_choice" =~ ^[nN] ]] && USE_INLINE_POLICIES=false || USE_INLINE_POLICIES=true

  # Setup Terraform working directories (from templates)
  TF_BACKEND_DIR="$SCAFFOLD_ROOT/templates/terraform/backend"
  TF_IAM_DIR="$SCAFFOLD_ROOT/templates/terraform/iam"

  # ===========================================================================
  # Step 3: Provision Resources
  # ===========================================================================
  header "Provisioning"
  echo ""

  # Initialize Terraform in both backend and IAM modules
  # Using -backend=false because we manage these resources directly, not via remote state
  terraform -chdir="$TF_BACKEND_DIR" init -reconfigure -backend=false -input=false -no-color &>/dev/null
  terraform -chdir="$TF_IAM_DIR"    init -reconfigure -backend=false -input=false -no-color &>/dev/null

  # Create shared backend resources (S3 bucket and DynamoDB table)
  S3_BUCKET=$(s3_bucket_name)
  DYNAMO_TABLE=$(dynamo_table_name)

  create_s3_bucket "$S3_BUCKET"
  ok "S3 bucket: $S3_BUCKET"

  create_dynamodb_table "$DYNAMO_TABLE"
  ok "DynamoDB table: $DYNAMO_TABLE"

  # Initialize the configuration file
  init_config

  # ===========================================================================
  # Step 4: Per-Environment Setup
  # ===========================================================================
  # Create IAM role, workflow, and providers.tf for each environment
  for (( i=0; i<${#ENV_NAMES[@]}; i++ )); do
    local_env="${ENV_NAMES[$i]}"
    local_dir="${ENV_DIRS[$i]}"
    local_branch="${ENV_BRANCHES[$i]}"

    # Determine role name (shared or per-environment)
    if [[ "$SHARED_ROLE" == "true" && $i -gt 0 ]]; then
      role_name=$(iam_role_name)
    else
      # Single env: github-actions-{repo}
      # Multi-env: github-actions-{repo}-{env}
      if [[ ${#ENV_NAMES[@]} -gt 1 ]]; then
        role_name="github-actions-${GITHUB_REPO}-${local_env}"
      else
        role_name="$(iam_role_name)"
      fi
    fi

    # Create IAM role with OIDC trust policy
    create_iam_role "$role_name" "$local_env"
    ok "IAM role: $role_name"

    # Construct the role ARN for the workflow
    role_arn="arn:aws:iam::${AWS_ACCOUNT_ID}:role/${role_name}"

    # Generate workflow and providers.tf
    generate_workflow "$local_env" "$local_dir" "$local_branch" "$role_arn"
    generate_providers_tf "$local_dir"
    add_env_to_config "$local_env" "$local_dir" "$local_branch"
  done

  ok "Config: $CONFIG_FILE"

  # ===========================================================================
  # Step 5: Show Next Steps
  # ===========================================================================
  echo ""
  header "Next Steps"
  echo ""
  echo "  1. Review:  git status"
  echo "  2. Commit:  git add . && git commit -m \"feat: add Scaffold\""
  echo "  3. Push:    git push origin main"
  echo ""
}

main "$@"
