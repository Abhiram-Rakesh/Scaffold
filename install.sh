#!/usr/bin/env bash
# =============================================================================
# install.sh - Install Scaffold CLI permanently
# =============================================================================
# This script installs Scaffold to ~/.local/bin and adds it to PATH.
# It can also install completions for bash/zsh.
#
# Usage:
#   ./install.sh              # Install to ~/.local/bin
#   ./install.sh --prefix=/usr/local  # Install to custom location
#   ./install.sh --help       # Show help
# =============================================================================

set -euo pipefail

SCAFFOLD_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_PREFIX="${HOME}/.local"
PREFIX="$DEFAULT_PREFIX"
INSTALL_BIN_DIR="$PREFIX/bin"
SHAREDIR="$PREFIX/share/scaffold"
VERBOSE=false

usage() {
    cat <<EOF
Scaffold Installer

Usage: $(basename "$0") [OPTIONS]

Options:
  --prefix PREFIX    Installation prefix (default: ~/.local)
  --system           Install system-wide (requires root)
  -v, --verbose      Verbose output
  -h, --help         Show this help message

Examples:
  $(basename "$0")                     # Install to ~/.local/bin
  sudo $(basename "$0") --system      # Install system-wide
  $(basename "$0") --prefix=/opt      # Install to /opt/bin
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --prefix)
            PREFIX="$2"
            INSTALL_BIN_DIR="$PREFIX/bin"
            SHAREDIR="$PREFIX/share/scaffold"
            shift 2
            ;;
        --system)
            PREFIX="/usr/local"
            INSTALL_BIN_DIR="$PREFIX/bin"
            SHAREDIR="$PREFIX/share/scaffold"
            ;;
        -v|--verbose)
            VERBOSE=true
            shift
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
log_err() { echo -e "${RED}[ERROR]${RESET} $*"; exit 1; }

echo ""
echo -e "${BOLD}╭─────────────────────────────────────╮${RESET}"
echo -e "${BOLD}│        Scaffold Installer             │${RESET}"
echo -e "${BOLD}╰─────────────────────────────────────╯${RESET}"
echo ""

# Check if running as root for system install
if [[ "$PREFIX" == "/usr/local" || "$PREFIX" == "/usr" ]]; then
    if [[ $EUID -ne 0 ]]; then
        log_err "System-wide install requires root. Use sudo or --prefix for custom location."
    fi
fi

# Check if scaffold directory exists
if [[ ! -d "$SCAFFOLD_ROOT/bin" || ! -d "$SCAFFOLD_ROOT/lib" ]]; then
    log_err "Invalid scaffold directory. Run this script from the scaffold repository."
fi

# Create installation directories
log_info "Creating directories..."
mkdir -p "$INSTALL_BIN_DIR" "$SHAREDIR"

# Copy files
log_info "Installing Scaffold to $INSTALL_BIN_DIR..."
cp -r "$SCAFFOLD_ROOT/bin" "$SHAREDIR/"
cp -r "$SCAFFOLD_ROOT/lib" "$SHAREDIR/"
cp -r "$SCAFFOLD_ROOT/templates" "$SHAREDIR/"
cp -r "$SCAFFOLD_ROOT/.scaffold" "$SHAREDIR/"

# Create wrapper script that points to shared installation
cat > "$INSTALL_BIN_DIR/scaffold" <<'EOF'
#!/usr/bin/env bash
# Wrapper script that calls the installed scaffold
SCAFFOLD_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../share/scaffold" && pwd)"
exec "$SCAFFOLD_ROOT/bin/scaffold" "$@"
EOF
chmod +x "$INSTALL_BIN_DIR/scaffold"

log_ok "Installed scaffold to $INSTALL_BIN_DIR/scaffold"

# Detect shell and add to PATH
SHELL_CONFIG=""
if [[ -n "${BASH_VERSION:-}" ]]; then
    SHELL_CONFIG="$HOME/.bashrc"
elif [[ -n "${ZSH_VERSION:-}" ]]; then
    SHELL_CONFIG="$HOME/.zshrc"
fi

# Add to PATH if not already there
PATH_LINE="export PATH=\"$INSTALL_BIN_DIR:\$PATH\""
if [[ -n "$SHELL_CONFIG" && -f "$SHELL_CONFIG" ]]; then
    if ! grep -q "$INSTALL_BIN_DIR" "$SHELL_CONFIG" 2>/dev/null; then
        echo "" >> "$SHELL_CONFIG"
        echo "# Scaffold CLI" >> "$SHELL_CONFIG"
        echo "$PATH_LINE" >> "$SHELL_CONFIG"
        log_ok "Added to PATH in $SHELL_CONFIG"
        log_warn "Run 'source $SHELL_CONFIG' or restart your terminal"
    else
        log_ok "PATH already configured in $SHELL_CONFIG"
    fi
fi

# Check if scaffold is in PATH now
if [[ ":$PATH:" == *":$INSTALL_BIN_DIR:"* ]] || command -v scaffold &>/dev/null; then
    log_ok "Scaffold is ready to use!"
    echo ""
    echo "  Run: scaffold --help"
else
    echo ""
    echo -e "${YELLOW}To use scaffold, add to PATH:${RESET}"
    echo "  $PATH_LINE"
    echo ""
fi
