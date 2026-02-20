#!/usr/bin/env bash
# init.sh - scaffold init
# Provisions AWS resources and generates GitHub Actions workflows

set -euo pipefail

SCAFFOLD_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/common.sh
source "$SCAFFOLD_ROOT/lib/common.sh"

# ─── Flags ────────────────────────────────────────────────────────────────────
USE_INLINE_POLICIES=true
SHARED_ROLE=false

for arg in "$@"; do
  case "$arg" in
    --use-inline-policies=false) USE_INLINE_POLICIES=false ;;
    --shared-role)               SHARED_ROLE=true ;;
  esac
done

# ─── S3 Bucket ────────────────────────────────────────────────────────────────
create_s3_bucket() {
  local bucket="$1"

  if s3_bucket_exists "$bucket"; then
    warn "S3 bucket $bucket already exists — importing."
    terraform -chdir="$TF_BACKEND_DIR" import aws_s3_bucket.terraform_state "$bucket" 2>/dev/null || true
  else
    terraform -chdir="$TF_BACKEND_DIR" apply -auto-approve \
      -var="bucket_name=$bucket" \
      -var="aws_region=$AWS_REGION" \
      -target=aws_s3_bucket.terraform_state \
      -target=aws_s3_bucket_versioning.terraform_state \
      -target=aws_s3_bucket_server_side_encryption_configuration.terraform_state \
      -target=aws_s3_bucket_public_access_block.terraform_state \
      -target=aws_s3_bucket_lifecycle_configuration.terraform_state
  fi
}

# ─── DynamoDB Table ───────────────────────────────────────────────────────────
create_dynamodb_table() {
  local table="$1"

  if dynamo_table_exists "$table"; then
    warn "DynamoDB table $table already exists — importing."
    terraform -chdir="$TF_BACKEND_DIR" import aws_dynamodb_table.terraform_locks "$table" 2>/dev/null || true
  else
    terraform -chdir="$TF_BACKEND_DIR" apply -auto-approve \
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

provider "aws" {
  region = var.aws_region
}

variable "aws_region" {
  description = "AWS region for resources"
  type        = string
  default     = "$AWS_REGION"
}
EOF
  ok "providers.tf: $providers_file"
}

# ─── Main ─────────────────────────────────────────────────────────────────────
main() {
  banner
  detect_repo
  echo "  Auto-detected repository: ${GITHUB_ORG}/${GITHUB_REPO}"
  echo ""

  configure_aws

  # Region
  header "Terraform Configuration"
  echo ""
  read -rp "  Region [$( [[ -n "${AWS_DEFAULT_REGION:-}" ]] && echo "${AWS_DEFAULT_REGION}" || echo "us-east-1")]: " AWS_REGION
  AWS_REGION="${AWS_REGION:-${AWS_DEFAULT_REGION:-us-east-1}}"
  export AWS_REGION

  # Environments
  echo ""
  read -rp "  How many environments? [1]: " num_envs
  num_envs="${num_envs:-1}"

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

  # Inline policies
  echo ""
  header "IAM Policy Mode"
  read -rp "  Use inline policies (SCP-compliant)? [Y/n]: " inline_choice
  inline_choice="${inline_choice:-y}"
  [[ "$inline_choice" =~ ^[nN] ]] && USE_INLINE_POLICIES=false || USE_INLINE_POLICIES=true

  # Setup Terraform working dirs
  TF_BACKEND_DIR="$SCAFFOLD_ROOT/templates/terraform/backend"
  TF_IAM_DIR="$SCAFFOLD_ROOT/templates/terraform/iam"

  header "Provisioning"
  echo ""

  # Init terraform modules
  terraform -chdir="$TF_BACKEND_DIR" init -reconfigure -backend=false -input=false -no-color &>/dev/null
  terraform -chdir="$TF_IAM_DIR"    init -reconfigure -backend=false -input=false -no-color &>/dev/null

  # Create shared backend resources
  S3_BUCKET=$(s3_bucket_name)
  DYNAMO_TABLE=$(dynamo_table_name)

  create_s3_bucket "$S3_BUCKET"
  ok "S3 bucket: $S3_BUCKET"

  create_dynamodb_table "$DYNAMO_TABLE"
  ok "DynamoDB table: $DYNAMO_TABLE"

  # Init config
  init_config

  # Per-environment
  for (( i=0; i<${#ENV_NAMES[@]}; i++ )); do
    local_env="${ENV_NAMES[$i]}"
    local_dir="${ENV_DIRS[$i]}"
    local_branch="${ENV_BRANCHES[$i]}"

    if [[ "$SHARED_ROLE" == "true" && $i -gt 0 ]]; then
      role_name=$(iam_role_name)
    else
      role_name="$(iam_role_name)${num_envs -gt 1 && echo "-${local_env}" || echo ""}"
      # Simpler: single role name for single env, env-suffixed for multi
      if [[ ${#ENV_NAMES[@]} -gt 1 ]]; then
        role_name="github-actions-${GITHUB_REPO}-${local_env}"
      else
        role_name="$(iam_role_name)"
      fi
    fi

    create_iam_role "$role_name" "$local_env"
    ok "IAM role: $role_name"

    role_arn="arn:aws:iam::${AWS_ACCOUNT_ID}:role/${role_name}"

    generate_workflow "$local_env" "$local_dir" "$local_branch" "$role_arn"
    generate_providers_tf "$local_dir"
    add_env_to_config "$local_env" "$local_dir" "$local_branch"
  done

  ok "Config: $CONFIG_FILE"

  echo ""
  header "Next Steps"
  echo ""
  echo "  1. Review:  git status"
  echo "  2. Commit:  git add . && git commit -m \"feat: add Scaffold\""
  echo "  3. Push:    git push origin main"
  echo ""
}

main "$@"
