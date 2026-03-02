#!/usr/bin/env bash
set -euo pipefail

# Scaffold Installation Script
# Usage: curl -fsSL https://raw.githubusercontent.com/Abhiram-Rakesh/Scaffold/main/scripts/install.sh | bash

REPO="${SCAFFOLD_REPO:-Abhiram-Rakesh/Scaffold}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="scaffold"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

print_banner() {
  echo -e "${CYAN}"
  echo "╭──────────────────────────────────────╮"
  echo "│   Scaffold - Infrastructure CI/CD   │"
  echo "│   Installer                          │"
  echo "╰──────────────────────────────────────╯"
  echo -e "${NC}"
}

detect_platform() {
  local os arch

  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux)  os="linux" ;;
    darwin) os="darwin" ;;
    *) echo -e "${RED}Unsupported OS: $os${NC}" >&2; exit 1 ;;
  esac

  arch="$(uname -m)"
  case "$arch" in
    x86_64)  arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) echo -e "${RED}Unsupported architecture: $arch${NC}" >&2; exit 1 ;;
  esac

  echo "${os}_${arch}"
}

get_latest_version() {
  curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed -E 's/.*"v([^"]+)".*/\1/'
}

download_binary() {
  local version="$1"
  local platform="$2"
  local url="https://github.com/${REPO}/releases/download/v${version}/scaffold_${version}_${platform}.tar.gz"
  local tmp_dir
  tmp_dir="$(mktemp -d)"

  echo -e "  Downloading scaffold v${version} for ${platform}..."
  curl -sSfL "$url" | tar -xzf - -C "$tmp_dir"

  echo -e "  Installing to ${INSTALL_DIR}/${BINARY_NAME}..."
  if [ -w "$INSTALL_DIR" ]; then
    mv "${tmp_dir}/scaffold" "${INSTALL_DIR}/${BINARY_NAME}"
  else
    sudo mv "${tmp_dir}/scaffold" "${INSTALL_DIR}/${BINARY_NAME}"
  fi
  chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
  rm -rf "$tmp_dir"
}

verify_installation() {
  if command -v scaffold &>/dev/null; then
    local version
    version="$(scaffold version 2>/dev/null | head -1 | awk '{print $2}')"
    echo -e "${GREEN}✓ Scaffold ${version} installed successfully!${NC}"
  else
    echo -e "${RED}✗ Installation failed. '${INSTALL_DIR}' may not be in your PATH.${NC}"
    echo "  Add this to your shell profile:"
    echo "    export PATH=\"\$PATH:${INSTALL_DIR}\""
    exit 1
  fi
}

main() {
  print_banner

  echo "→ Detecting platform..."
  local platform
  platform="$(detect_platform)"
  echo -e "  ${GREEN}✓ Platform: ${platform}${NC}"

  echo ""
  echo "→ Fetching latest version..."
  local version
  version="$(get_latest_version)"
  echo -e "  ${GREEN}✓ Latest version: v${version}${NC}"

  echo ""
  echo "→ Installing scaffold..."
  download_binary "$version" "$platform"
  echo -e "  ${GREEN}✓ Installed to ${INSTALL_DIR}/${BINARY_NAME}${NC}"

  echo ""
  echo "→ Verifying installation..."
  verify_installation

  echo ""
  echo "→ Quick start:"
  echo -e "  ${CYAN}cd your-infra-repo${NC}"
  echo -e "  ${CYAN}scaffold init${NC}"
  echo ""
  echo "  Documentation: https://scaffold.sh/docs"
}

main "$@"
