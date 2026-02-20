#!/usr/bin/env bash
# destroy.sh - scaffold destroy
# Destroys Terraform-managed infrastructure for a given environment.
# Platform resources (S3 state, DynamoDB, IAM role) remain intact.

set -euo pipefail

SCAFFOLD_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/common.sh
source "$SCAFFOLD_ROOT/lib/common.sh"

# ─── Lock Helpers ────────────────────────────────────────────────────────────
check_and_remove_locks() {
  local state_key="$1"
  local s3_bucket; s3_bucket=$(read_config_field "s3_bucket")
  local dynamo_table; dynamo_table=$(read_config_field "dynamodb_table")

  info "Checking for state locks..."

  # DynamoDB lock key is: <bucket>/<state_key>-md5
  local lock_id="${s3_bucket}/${state_key}-md5"
  local lock_item
  lock_item=$(aws dynamodb get-item \
    --table-name "$dynamo_table" \
    --key "{\"LockID\":{\"S\":\"${lock_id}\"}}" \
    --output json 2>/dev/null || echo "{}")

  if echo "$lock_item" | python3 -c "import sys,json; d=json.load(sys.stdin); sys.exit(0 if d.get('Item') else 1)" 2>/dev/null; then
    echo ""
    warn "Found 1 active state lock(s)"
    echo "  Lock ID: $lock_id"
    echo ""
    echo "  This lock may be stale if:"
    echo "    - GitHub Actions workflow completed"
    echo "    - Pipeline crashed mid-apply"
    echo "    - No terraform operations running"
    echo ""
    read -rp "  Remove this lock? [y/N]: " remove_lock
    if [[ "$remove_lock" =~ ^[yY] ]]; then
      info "Removing stale lock..."
      aws dynamodb delete-item \
        --table-name "$dynamo_table" \
        --key "{\"LockID\":{\"S\":\"${lock_id}\"}}"
      ok "Lock removed. Continuing with destroy..."
    else
      die "Aborted. Please resolve the lock manually before retrying."
    fi
  else
    ok "No locks found"
  fi
}

# ─── Destroy Environment ─────────────────────────────────────────────────────
destroy_environment() {
  local env_name="$1" watch_dir="$2"

  local s3_bucket;    s3_bucket=$(read_config_field "s3_bucket")
  local dynamo_table; dynamo_table=$(read_config_field "dynamodb_table")
  local aws_region;   aws_region=$(read_config_field "aws_region")
  local state_key="${env_name}/terraform.tfstate"

  check_and_remove_locks "$state_key"

  if [[ ! -d "$watch_dir" ]]; then
    warn "Watch directory '$watch_dir' not found — skipping."
    return
  fi

  header "Generating destroy plan..."
  echo ""

  terraform -chdir="$watch_dir" init -reconfigure -input=false \
    -backend-config="bucket=${s3_bucket}" \
    -backend-config="key=${state_key}" \
    -backend-config="region=${aws_region}" \
    -backend-config="dynamodb_table=${dynamo_table}" \
    -backend-config="encrypt=true" \
    -no-color 2>&1 | grep -E "(Initializing|Error|Warning)" || true

  local plan_out
  plan_out=$(terraform -chdir="$watch_dir" plan -destroy -no-color 2>&1)

  # Check if there's anything to destroy
  if echo "$plan_out" | grep -q "No changes"; then
    ok "No resources to destroy in environment '$env_name'."
    remove_env_from_config "$env_name"
    return
  fi

  echo "$plan_out" | grep -E "^  # |Plan:" | head -40

  echo ""
  header "Resources to be destroyed:"
  echo ""
  echo "$plan_out" | grep "^  # " | sed 's/  # /  - /' | head -30
  echo ""

  read -rp "  Type DESTROY to confirm: " confirm
  [[ "$confirm" == "DESTROY" ]] || die "Aborted."

  header "Destroying..."
  local start=$SECONDS
  terraform -chdir="$watch_dir" destroy -auto-approve \
    -no-color 2>&1 | grep -E "(Destroying|Destroyed|Error|Complete)" || true

  local elapsed=$(( SECONDS - start ))
  ok "Complete (${elapsed}s)"

  remove_env_from_config "$env_name"
  echo ""
  echo "  Note: Platform resources (S3 state, IAM role) remain intact."
  echo "  Run \`scaffold uninstall\` to remove everything."
  echo ""
}

# ─── Main ─────────────────────────────────────────────────────────────────────
main() {
  banner

  [[ -f "$CONFIG_FILE" ]] || die "No .scaffold/config.json found. Run \`scaffold init\` first."

  configure_aws

  AWS_REGION=$(read_config_field "aws_region")
  export AWS_REGION

  header "Select Environment"
  echo ""

  mapfile -t envs < <(list_environments)

  if [[ ${#envs[@]} -eq 0 ]]; then
    die "No environments found in config."
  fi

  local i=1
  for env_line in "${envs[@]}"; do
    local env_name; env_name=$(echo "$env_line" | cut -d'|' -f1)
    local watch_dir; watch_dir=$(echo "$env_line" | cut -d'|' -f2)
    echo "  [$i] $env_name ($watch_dir)"
    (( i++ ))
  done
  echo "  [$i] All environments"

  echo ""
  read -rp "  Choice: " choice

  if [[ "$choice" -eq $i ]]; then
    # All environments
    for env_line in "${envs[@]}"; do
      local env_name; env_name=$(echo "$env_line" | cut -d'|' -f1)
      local watch_dir; watch_dir=$(echo "$env_line" | cut -d'|' -f2)
      destroy_environment "$env_name" "$watch_dir"
    done
  else
    local idx=$(( choice - 1 ))
    local env_line="${envs[$idx]}"
    local env_name; env_name=$(echo "$env_line" | cut -d'|' -f1)
    local watch_dir; watch_dir=$(echo "$env_line" | cut -d'|' -f2)
    destroy_environment "$env_name" "$watch_dir"
  fi
}

main "$@"
