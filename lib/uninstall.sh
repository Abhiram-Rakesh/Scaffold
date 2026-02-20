#!/usr/bin/env bash
# uninstall.sh - scaffold uninstall
# Destroys ALL Scaffold resources: infrastructure, platform, workflows, config.

set -euo pipefail

SCAFFOLD_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/common.sh
source "$SCAFFOLD_ROOT/lib/common.sh"

destroy_all_environments() {
  header "Destroying environments..."
  mapfile -t envs < <(list_environments)

  for env_line in "${envs[@]}"; do
    local env_name; env_name=$(echo "$env_line" | cut -d'|' -f1)
    local watch_dir; watch_dir=$(echo "$env_line" | cut -d'|' -f2)
    local state_key="${env_name}/terraform.tfstate"
    local s3_bucket;    s3_bucket=$(read_config_field "s3_bucket")
    local dynamo_table; dynamo_table=$(read_config_field "dynamodb_table")
    local aws_region;   aws_region=$(read_config_field "aws_region")

    if [[ ! -d "$watch_dir" ]]; then
      warn "Watch dir '$watch_dir' not found — skipping env '$env_name'."
      continue
    fi

    terraform -chdir="$watch_dir" init -reconfigure -input=false \
      -backend-config="bucket=${s3_bucket}" \
      -backend-config="key=${state_key}" \
      -backend-config="region=${aws_region}" \
      -backend-config="dynamodb_table=${dynamo_table}" \
      -backend-config="encrypt=true" \
      -no-color &>/dev/null || true

    if ! terraform -chdir="$watch_dir" plan -destroy -no-color 2>&1 | grep -q "No changes"; then
      terraform -chdir="$watch_dir" destroy -auto-approve -no-color &>/dev/null || true
    fi

    ok "$env_name infrastructure destroyed"
  done
}

destroy_platform() {
  local s3_bucket;    s3_bucket=$(read_config_field "s3_bucket")
  local dynamo_table; dynamo_table=$(read_config_field "dynamodb_table")
  local iam_role;     iam_role=$(read_config_field "iam_role")
  local aws_region;   aws_region=$(read_config_field "aws_region")

  header "Destroying platform..."

  # IAM roles (may be multiple for multi-env)
  for role in $(aws iam list-roles --query "Roles[?starts_with(RoleName, 'github-actions-')].RoleName" \
                  --output text 2>/dev/null | tr '\t' '\n' | grep "github-actions-${GITHUB_REPO}" || true); do
    # Detach/delete policies
    for p in $(aws iam list-role-policies --role-name "$role" --query PolicyNames --output text 2>/dev/null || true); do
      aws iam delete-role-policy --role-name "$role" --policy-name "$p" 2>/dev/null || true
    done
    aws iam delete-role --role-name "$role" 2>/dev/null || true
    ok "IAM role deleted: $role"
  done

  # DynamoDB
  if dynamo_table_exists "$dynamo_table"; then
    aws dynamodb delete-table --table-name "$dynamo_table" --no-cli-pager &>/dev/null
    aws dynamodb wait table-not-exists --table-name "$dynamo_table" 2>/dev/null || true
    ok "DynamoDB table deleted"
  fi

  # S3: empty then delete
  if s3_bucket_exists "$s3_bucket"; then
    info "Emptying S3 bucket (including all versions)..."
    # Delete all versions and delete markers
    aws s3api list-object-versions --bucket "$s3_bucket" --output json 2>/dev/null \
      | python3 - "$s3_bucket" <<'PYEOF'
import json, sys, subprocess
bucket = sys.argv[1]
data = json.load(sys.stdin)
objects = []
for v in data.get("Versions", []):
    objects.append({"Key": v["Key"], "VersionId": v["VersionId"]})
for m in data.get("DeleteMarkers", []):
    objects.append({"Key": m["Key"], "VersionId": m["VersionId"]})
for i in range(0, len(objects), 1000):
    batch = objects[i:i+1000]
    delete = json.dumps({"Objects": batch, "Quiet": True})
    subprocess.run(["aws", "s3api", "delete-objects", "--bucket", bucket,
                    "--delete", delete], check=False, capture_output=True)
PYEOF
    aws s3 rb "s3://$s3_bucket" --force &>/dev/null
    ok "S3 bucket emptied and deleted"
  fi

  # Workflows
  if ls .github/workflows/terraform-*.yaml &>/dev/null 2>&1; then
    rm -f .github/workflows/terraform-*.yaml
    ok "Workflows removed"
  fi

  # Config dir
  if [[ -d ".scaffold" ]]; then
    rm -rf ".scaffold"
    ok ".scaffold/ removed"
  fi
}

main() {
  banner

  [[ -f "$CONFIG_FILE" ]] || die "No .scaffold/config.json found."

  echo -e "  ${RED}⚠️  WARNING: This will destroy ALL Scaffold resources:${RESET}"
  echo "    - S3 state bucket (including all state history)"
  echo "    - DynamoDB lock table"
  echo "    - IAM OIDC role(s)"
  echo "    - All workflows"
  echo "    - Configuration files"
  echo ""

  read -rp "  Type DESTROY EVERYTHING to confirm: " confirm
  [[ "$confirm" == "DESTROY EVERYTHING" ]] || die "Aborted."

  configure_aws
  detect_repo

  AWS_REGION=$(read_config_field "aws_region")
  export AWS_REGION

  destroy_all_environments
  destroy_platform

  echo ""
  ok "Uninstall complete"
  echo ""
}

main "$@"
