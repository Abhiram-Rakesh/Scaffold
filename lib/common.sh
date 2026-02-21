#!/usr/bin/env bash
# =============================================================================
# common.sh - Shared utilities for Scaffold
# =============================================================================
# Contains common functions used across all scaffold commands:
# - UI helpers (colors, banners, messages)
# - Git repository detection
# - AWS credential configuration
# - Resource naming conventions
# - Configuration file management
# - AWS resource existence checks
# =============================================================================

set -euo pipefail

# =============================================================================
# UI / Color Codes
# Terminal escape codes for colored output
# =============================================================================
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

# Print functions for different message types
ok() { echo -e "  ${GREEN}✓${RESET}  $*"; }         # Success message (green checkmark)
info() { echo -e "  ${BLUE}→${RESET}  $*"; }        # Info message (blue arrow)
warn() { echo -e "  ${YELLOW}[WARN]${RESET} $*"; }  # Warning message (yellow)
err() { echo -e "  ${RED}[ERROR]${RESET} $*" >&2; } # Error message (red)
die() {
    err "$*"
    exit 1
} # Print error and exit

# Section header - prints a cyan heading
header() {
    echo ""
    echo -e "${CYAN}→ $*${RESET}"
}

# ASCII art banner shown at the start of each command
banner() {
    echo ""
    echo -e "${BOLD}${CYAN}╭─────────────────────────────────────╮${RESET}"
    echo -e "${BOLD}${CYAN}│   Scaffold - Infrastructure CI/CD   │${RESET}"
    echo -e "${BOLD}${CYAN}╰─────────────────────────────────────╯${RESET}"
    echo ""
}

# =============================================================================
# Configuration Paths
# =============================================================================
SCAFFOLD_DIR=".scaffold"                # Directory for scaffold config
CONFIG_FILE="$SCAFFOLD_DIR/config.json" # Main configuration file

# =============================================================================
# Git / Repository Detection
# =============================================================================
# Extracts GitHub organization and repository name from the 'origin' remote URL
detect_repo() {
    local repo_url
    # Get the origin remote URL; fail if not a git repo or no origin
    repo_url=$(git remote get-url origin 2>/dev/null) || die "Not a git repo or no 'origin' remote found."

    # Extract org and repo from URL patterns like:
    # https://github.com/org/repo.git or git@github.com:org/repo.git
    GITHUB_ORG=$(echo "$repo_url" | sed -n 's/.*[:/]\([^/]*\)\/\([^/.]*\).*/\1/p')
    GITHUB_REPO=$(echo "$repo_url" | sed -n 's/.*[:/]\([^/]*\)\/\([^/.]*\).*/\2/p')

    [[ -n "$GITHUB_ORG" ]] || die "Could not detect GitHub org from remote URL: $repo_url"
    [[ -n "$GITHUB_REPO" ]] || die "Could not detect GitHub repo from remote URL: $repo_url"
}

# =============================================================================
# AWS Credentials Configuration
# =============================================================================
# Prompts user for AWS credentials or uses environment variables
# Supports: AWS CLI profile, access keys, or environment variables
configure_aws() {
    header "AWS Configuration"

    # First check for AWS credentials in environment variables
    if [[ -n "${AWS_ACCESS_KEY_ID:-}" && -n "${AWS_SECRET_ACCESS_KEY:-}" ]]; then
        info "Using AWS credentials from environment variables."
        AWS_PROFILE_MODE="env"
        return
    fi

    # Interactive credential selection
    echo ""
    echo "  AWS Credentials:"
    echo "  [1] Use existing AWS CLI profile"
    echo "  [2] Enter access key/secret (stored in memory only)"
    read -rp "  Choice [1]: " cred_choice
    cred_choice="${cred_choice:-1}"

    case "$cred_choice" in
    1)
        # Use existing AWS CLI profile
        read -rp "  Profile [default]: " AWS_PROFILE
        AWS_PROFILE="${AWS_PROFILE:-default}"
        export AWS_PROFILE
        AWS_PROFILE_MODE="profile"
        ;;
    2)
        # Enter access key and secret directly (stored in memory only, not disk)
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

    # Verify credentials by calling STS GetCallerIdentity
    echo ""
    info "Verifying credentials..."
    local caller_arn
    caller_arn=$(aws sts get-caller-identity --query Arn --output text 2>/dev/null) ||
        die "AWS credential verification failed. Check your credentials."
    ok "Authenticated as: $caller_arn"

    # Extract and export AWS account ID for use in ARN constructions
    AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
    export AWS_ACCOUNT_ID
}

# =============================================================================
# Resource Naming Conventions
# =============================================================================
# Generates consistent, unique resource names based on org/repo
# Uses a short hash to avoid name collisions while keeping names readable

# Generate a stable 8-character hash from org/repo for uniqueness
resource_hash() {
    local input="${GITHUB_ORG}/${GITHUB_REPO}"
    echo -n "$input" | sha256sum | cut -c1-8
}

# S3 bucket name for storing Terraform state
# Format: tf-state-{repo}-{account_id}-{hash}
s3_bucket_name() { echo "tf-state-${GITHUB_REPO}-${AWS_ACCOUNT_ID}-$(resource_hash)"; }

# DynamoDB table name for state locking
# Format: tf-lock-{repo}-{account_id}-{hash}
dynamo_table_name() { echo "tf-lock-${GITHUB_REPO}-${AWS_ACCOUNT_ID}-$(resource_hash)"; }

# IAM role name for GitHub Actions OIDC
# Format: github-actions-{repo}
iam_role_name() { echo "github-actions-${GITHUB_REPO}"; }

# =============================================================================
# Configuration File Management
# =============================================================================
# Scaffold stores its configuration in .scaffold/config.json
# This tracks all environments, resources, and settings

# Initialize the configuration file with default values
init_config() {
    mkdir -p "$SCAFFOLD_DIR"
    if [[ ! -f "$CONFIG_FILE" ]]; then
        cat >"$CONFIG_FILE" <<EOF
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

# Read a specific field from the config file
read_config_field() {
    local field="$1"
    python3 -c "import json,sys; d=json.load(open('$CONFIG_FILE')); print(d.get('$field',''))"
}

# Add or update an environment in the config file
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

# Remove an environment from the config file
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

# List all environments from config, formatted as: name|watch_dir|branch
list_environments() {
    python3 -c "
import json
with open('$CONFIG_FILE') as f:
    cfg = json.load(f)
for e in cfg.get('environments', []):
    print(e['name'] + '|' + e['watch_dir'] + '|' + e['branch'])
"
}

# =============================================================================
# AWS Resource Existence Checks
# =============================================================================
# Helper functions to check if AWS resources already exist
# Used to determine whether to create new resources or import existing ones

# Check if an S3 bucket exists (by attempting to get its metadata)
s3_bucket_exists() { 
    aws s3api head-bucket --bucket "$1" &>/dev/null
}

# Check if a DynamoDB table exists
dynamo_table_exists() {
    aws dynamodb describe-table --table-name "$1" --output text &>/dev/null
}

# Check if an IAM role exists
iam_role_exists() {
    aws iam get-role --role-name "$1" --output text &>/dev/null
}
