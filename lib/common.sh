#!/usr/bin/env bash
# common.sh - Shared utilities for Scaffold

set -euo pipefail

# ─── Colors & UI ─────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

ok()   { echo -e "  ${GREEN}✓${RESET}  $*"; }
info() { echo -e "  ${BLUE}→${RESET}  $*"; }
warn() { echo -e "  ${YELLOW}[WARN]${RESET} $*"; }
err()  { echo -e "  ${RED}[ERROR]${RESET} $*" >&2; }
die()  { err "$*"; exit 1; }

header() {
  echo ""
  echo -e "${CYAN}→ $*${RESET}"
}

banner() {
  echo ""
  echo -e "${BOLD}${CYAN}╭─────────────────────────────────────╮${RESET}"
  echo -e "${BOLD}${CYAN}│   Scaffold - Infrastructure CI/CD  │${RESET}"
  echo -e "${BOLD}${CYAN}╰─────────────────────────────────────╯${RESET}"
  echo ""
}

# ─── Config ──────────────────────────────────────────────────────────────────
SCAFFOLD_DIR=".scaffold"
CONFIG_FILE="$SCAFFOLD_DIR/config.json"

# ─── Git / Repo Detection ─────────────────────────────────────────────────────
detect_repo() {
  local repo_url
  repo_url=$(git remote get-url origin 2>/dev/null) || die "Not a git repo or no 'origin' remote found."
  GITHUB_ORG=$(echo "$repo_url"  | sed -n 's/.*[:/]\([^/]*\)\/\([^/.]*\).*/\1/p')
  GITHUB_REPO=$(echo "$repo_url" | sed -n 's/.*[:/]\([^/]*\)\/\([^/.]*\).*/\2/p')
  [[ -n "$GITHUB_ORG"  ]] || die "Could not detect GitHub org from remote URL: $repo_url"
  [[ -n "$GITHUB_REPO" ]] || die "Could not detect GitHub repo from remote URL: $repo_url"
}

# ─── AWS Credentials ─────────────────────────────────────────────────────────
configure_aws() {
  header "AWS Configuration"

  # Try environment variables first
  if [[ -n "${AWS_ACCESS_KEY_ID:-}" && -n "${AWS_SECRET_ACCESS_KEY:-}" ]]; then
    info "Using AWS credentials from environment variables."
    AWS_PROFILE_MODE="env"
    return
  fi

  echo ""
  echo "  AWS Credentials:"
  echo "  [1] Use existing AWS CLI profile"
  echo "  [2] Enter access key/secret (stored in memory only)"
  read -rp "  Choice [1]: " cred_choice
  cred_choice="${cred_choice:-1}"

  case "$cred_choice" in
    1)
      read -rp "  Profile [default]: " AWS_PROFILE
      AWS_PROFILE="${AWS_PROFILE:-default}"
      export AWS_PROFILE
      AWS_PROFILE_MODE="profile"
      ;;
    2)
      read -rp "  AWS Access Key ID: " AWS_ACCESS_KEY_ID
      read -rsp "  AWS Secret Access Key: " AWS_SECRET_ACCESS_KEY
      echo ""
      read -rp "  AWS Session Token (leave blank if none): " AWS_SESSION_TOKEN
      export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY
      [[ -n "$AWS_SESSION_TOKEN" ]] && export AWS_SESSION_TOKEN
      AWS_PROFILE_MODE="keys"
      ;;
    *) die "Invalid choice." ;;
  esac

  echo ""
  info "Verifying credentials..."
  local caller_arn
  caller_arn=$(aws sts get-caller-identity --query Arn --output text 2>/dev/null) \
    || die "AWS credential verification failed. Check your credentials."
  ok "Authenticated as: $caller_arn"
  AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
  export AWS_ACCOUNT_ID
}

# ─── Resource Naming ─────────────────────────────────────────────────────────
# Stable 8-char hash from org+repo
resource_hash() {
  local input="${GITHUB_ORG}/${GITHUB_REPO}"
  echo -n "$input" | sha256sum | cut -c1-8
}

s3_bucket_name()   { echo "tf-state-${GITHUB_REPO}-$(resource_hash)"; }
dynamo_table_name(){ echo "tf-lock-${GITHUB_REPO}-$(resource_hash)"; }
iam_role_name()    { echo "github-actions-${GITHUB_REPO}"; }

# ─── Config File ─────────────────────────────────────────────────────────────
init_config() {
  mkdir -p "$SCAFFOLD_DIR"
  if [[ ! -f "$CONFIG_FILE" ]]; then
    cat > "$CONFIG_FILE" <<EOF
{
  "version": "1",
  "repo": "${GITHUB_ORG}/${GITHUB_REPO}",
  "aws_region": "${AWS_REGION}",
  "s3_bucket": "$(s3_bucket_name)",
  "dynamodb_table": "$(dynamo_table_name)",
  "iam_role": "$(iam_role_name)",
  "environments": []
}
EOF
  fi
}

read_config_field() {
  local field="$1"
  python3 -c "import json,sys; d=json.load(open('$CONFIG_FILE')); print(d.get('$field',''))"
}

add_env_to_config() {
  local env_name="$1" watch_dir="$2" branch="$3"
  python3 - <<PYEOF
import json, sys
with open("$CONFIG_FILE") as f:
    cfg = json.load(f)
envs = cfg.setdefault("environments", [])
# Remove if already exists
envs = [e for e in envs if e["name"] != "$env_name"]
envs.append({"name": "$env_name", "watch_dir": "$watch_dir", "branch": "$branch"})
cfg["environments"] = envs
with open("$CONFIG_FILE", "w") as f:
    json.dump(cfg, f, indent=2)
PYEOF
}

remove_env_from_config() {
  local env_name="$1"
  python3 - <<PYEOF
import json
with open("$CONFIG_FILE") as f:
    cfg = json.load(f)
cfg["environments"] = [e for e in cfg.get("environments", []) if e["name"] != "$env_name"]
with open("$CONFIG_FILE", "w") as f:
    json.dump(cfg, f, indent=2)
PYEOF
}

list_environments() {
  python3 -c "
import json
with open('$CONFIG_FILE') as f:
    cfg = json.load(f)
for e in cfg.get('environments', []):
    print(e['name'] + '|' + e['watch_dir'] + '|' + e['branch'])
"
}

# ─── AWS Resource Helpers ─────────────────────────────────────────────────────
s3_bucket_exists() { aws s3api head-bucket --bucket "$1" 2>/dev/null; }
dynamo_table_exists() {
  aws dynamodb describe-table --table-name "$1" --output text &>/dev/null
}
iam_role_exists() {
  aws iam get-role --role-name "$1" --output text &>/dev/null
}
