#!/usr/bin/env bash
# =============================================================================
# uninstall.sh - Uninstall Scaffold CLI from host
# =============================================================================
# This script removes Scaffold from the system.
#
# Usage:
#   ./uninstall.sh              # Uninstall from ~/.local
#   ./uninstall.sh --prefix=/usr/local  # Uninstall from custom location
#   ./uninstall.sh --system      # Uninstall system-wide
# =============================================================================

set -euo pipefail

PREFIX="${HOME}/.local"
SHAREDIR="$PREFIX/share/scaffold"
BIN_DIR="$PREFIX/bin"

usage() {
    cat <<EOF
Scaffold Uninstaller

Usage: $(basename "$0") [OPTIONS]

Options:
  --prefix PREFIX    Installation prefix (default: ~/.local)
  --system           Uninstall system-wide (requires root)
  -h, --help         Show this help message

Examples:
  $(basename "$0")              # Uninstall from ~/.local
  sudo $(basename "$0") --system   # Uninstall system-wide
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --prefix)
            PREFIX="$2"
            SHAREDIR="$PREFIX/share/scaffold"
            BIN_DIR="$PREFIX/bin"
            shift 2
            ;;
        --system)
            PREFIX="/usr/local"
            SHAREDIR="$PREFIX/share/scaffold"
            BIN_DIR="$PREFIX/bin"
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Check if running as root for system uninstall
if [[ "$PREFIX" == "/usr/local" || "$PREFIX" == "/usr" ]]; then
    if [[ $EUID -ne 0 ]]; then
        echo "Error: System-wide uninstall requires root. Use sudo."
        exit 1
    fi
fi

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

log_info() { echo -e "${CYAN}→${RESET} $*"; }
log_ok() { echo -e "${GREEN}✓${RESET} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${RESET} $*"; }

echo ""
echo -e "${BOLD}╭─────────────────────────────────────╮${RESET}"
echo -e "${BOLD}│        Scaffold Uninstaller          │${RESET}"
echo -e "${BOLD}╰─────────────────────────────────────╯${RESET}"
echo ""

# Check if installed
if [[ ! -d "$SHAREDIR" && ! -f "$BIN_DIR/scaffold" ]]; then
    log_warn "Scaffold is not installed at $PREFIX"
    exit 0
fi

# Remove binary
if [[ -f "$BIN_DIR/scaffold" ]]; then
    rm -f "$BIN_DIR/scaffold"
    log_ok "Removed $BIN_DIR/scaffold"
fi

# Remove shared files
if [[ -d "$SHAREDIR" ]]; then
    rm -rf "$SHAREDIR"
    log_ok "Removed $SHAREDIR"
fi

echo ""
log_ok "Scaffold has been uninstalled"
echo ""
echo "Note: You may need to manually remove the PATH line from your shell config:"
echo "  ~/.bashrc or ~/.zshrc"
echo ""
echo "The line looks like: export PATH=\"$PREFIX/bin:\$PATH\""
