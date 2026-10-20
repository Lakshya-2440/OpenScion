#!/usr/bin/env bash
# OpenScion One-Line Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/Lakshya-2440/OpenScion/main/install.sh | bash

set -e

REPO="Lakshya-2440/OpenScion"
BINARY_NAME="scion"
ALIAS_NAME="scn"
VERSION="${OPENSCION_VERSION:-latest}"

BOLD="\033[1m"
GREEN="\033[32m"
CYAN="\033[36m"
YELLOW="\033[33m"
RED="\033[31m"
RESET="\033[0m"

echo -e "${CYAN}${BOLD}"
cat << "EOF"
   ___                   ____       _             
  / _ \ _ __   ___ _ __ / ___|  ___(_) ___  _ __  
 | | | | '_ \ / _ \ '_ \\___ \ / __| |/ _ \| '_ \ 
 | |_| | |_) |  __/ | | |___) | (__| | (_) | | | |
  \___/| .__/ \___|_| |_|____/ \___|_|\___/|_| |_|
       |_|                                        
EOF
echo -e "${RESET}"
echo -e "${BOLD}Agent-Scale Terminal Version Control System${RESET}"
echo "--------------------------------------------------------"

# 1. Detect OS & Architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo -e "${RED}Error: Unsupported architecture $ARCH${RESET}"
    exit 1
    ;;
esac

case "$OS" in
  darwin) PLATFORM="darwin" ;;
  linux) PLATFORM="linux" ;;
  mingw*|msys*|cygwin*|windows*)
    PLATFORM="windows"
    BINARY_NAME="scion.exe"
    ALIAS_NAME="scn.exe"
    ;;
  *)
    echo -e "${RED}Error: Unsupported OS $OS${RESET}"
    exit 1
    ;;
esac

TARGET="${PLATFORM}_${ARCH}"
echo -e "Detected target platform: ${CYAN}${TARGET}${RESET}"

# 2. Determine Install Directory
if [ -w "/usr/local/bin" ]; then
  INSTALL_DIR="/usr/local/bin"
else
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

TEMP_DIR=$(mktemp -d)
cleanup() {
  rm -rf "$TEMP_DIR"
}
trap cleanup EXIT

INSTALLED=0

# 3. Attempt downloading prebuilt release from GitHub
RELEASE_URL="https://github.com/${REPO}/releases/latest/download/scion-${TARGET}.tar.gz"
echo -e "Attempting prebuilt release download..."

if curl -s -f -L "$RELEASE_URL" -o "$TEMP_DIR/scion.tar.gz" 2>/dev/null; then
  tar -xzf "$TEMP_DIR/scion.tar.gz" -C "$TEMP_DIR"
  cp "$TEMP_DIR/scion" "$INSTALL_DIR/$BINARY_NAME"
  chmod +x "$INSTALL_DIR/$BINARY_NAME"
  ln -sf "$INSTALL_DIR/$BINARY_NAME" "$INSTALL_DIR/$ALIAS_NAME"
  INSTALLED=1
  echo -e "${GREEN}✓ Downloaded and installed release binary.${RESET}"
fi

# 4. Fallback: Build from source if Go is present
if [ "$INSTALLED" -eq 0 ]; then
  echo -e "${YELLOW}Release binary not available directly (repo may be private or unreleased).${RESET}"
  if command -v go >/dev/null 2>&1; then
    echo -e "Found Go $(go version | awk '{print $3}'). Compiling from source..."
    git clone --depth 1 "https://github.com/${REPO}.git" "$TEMP_DIR/src" 2>/dev/null || {
      # If git clone fails due to private auth, check if current directory is the repo
      if [ -f "./cmd/scion/main.go" ]; then
        cp -r . "$TEMP_DIR/src"
      else
        echo -e "${RED}Failed to clone repository. If repository is private, ensure your SSH key or git credentials are configured.${RESET}"
        exit 1
      fi
    }
    (cd "$TEMP_DIR/src" && go build -ldflags="-s -w" -o "$TEMP_DIR/scion" ./cmd/scion)
    cp "$TEMP_DIR/scion" "$INSTALL_DIR/$BINARY_NAME"
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    ln -sf "$INSTALL_DIR/$BINARY_NAME" "$INSTALL_DIR/$ALIAS_NAME"
    INSTALLED=1
    echo -e "${GREEN}✓ Successfully compiled and installed OpenScion.${RESET}"
  else
    echo -e "${RED}Error: Neither prebuilt binary nor Go compiler found.${RESET}"
    echo -e "Please install Go (https://go.dev) or download release from https://github.com/${REPO}/releases"
    exit 1
  fi
fi

# Ad-hoc codesign on macOS to prevent SIGKILL 137
if [ "$PLATFORM" = "darwin" ] && command -v codesign >/dev/null 2>&1; then
  codesign -s - -f "$INSTALL_DIR/$BINARY_NAME" 2>/dev/null || true
fi

# 5. Check PATH and configure shell profile if needed
echo ""
echo -e "${GREEN}${BOLD}✓ OpenScion installed to: ${INSTALL_DIR}/${BINARY_NAME}${RESET}"
echo -e "${GREEN}${BOLD}✓ Alias linked: ${INSTALL_DIR}/${ALIAS_NAME}${RESET}"

PATH_NEEDS_UPDATE=1
case ":$PATH:" in
  *":$INSTALL_DIR:"*) PATH_NEEDS_UPDATE=0 ;;
esac

if [ "$PATH_NEEDS_UPDATE" -eq 1 ]; then
  SHELL_NAME="$(basename "$SHELL")"
  PROFILE=""
  if [ "$SHELL_NAME" = "zsh" ]; then
    PROFILE="$HOME/.zshrc"
  elif [ "$SHELL_NAME" = "bash" ]; then
    [ -f "$HOME/.bashrc" ] && PROFILE="$HOME/.bashrc" || PROFILE="$HOME/.bash_profile"
  fi

  if [ -n "$PROFILE" ]; then
    echo "export PATH=\"\$PATH:$INSTALL_DIR\"" >> "$PROFILE"
    echo -e "${CYAN}Added $INSTALL_DIR to $PROFILE${RESET}"
    echo -e "${YELLOW}Run: source $PROFILE (or restart terminal)${RESET}"
  else
    echo -e "${YELLOW}Please add $INSTALL_DIR to your PATH manually.${RESET}"
  fi
fi

# 6. Configure AI Coding Agents (Claude Code, Cursor, Aider)
AGENT_CONFIGURED=0
CLAUDE_DIR="$HOME/.claude"
if [ -d "$CLAUDE_DIR" ]; then
  CLAUDE_GLOBAL="$CLAUDE_DIR/CLAUDE.md"
  if ! grep -q "OpenScion" "$CLAUDE_GLOBAL" 2>/dev/null; then
    cat << 'EOF' >> "$CLAUDE_GLOBAL"

## OpenScion Version Control (scn)
When working in any project with a `.scion/` directory:
- Use `scn status` (instead of `git status`)
- Use `scn diff` (instead of `git diff`)
- Use `scn commit -m "<message>" --agent <model>` (instead of `git commit`)
- Use `scn log` (instead of `git log`)
- Use `scn pull` and `scn push` for synchronization
EOF
    AGENT_CONFIGURED=1
  fi
fi

if [ "$AGENT_CONFIGURED" -eq 1 ]; then
  echo -e "${GREEN}${BOLD}✓ Auto-configured AI coding agents (Claude Code) to use OpenScion (scn).${RESET}"
fi

echo ""
echo -e "${BOLD}Get Started:${RESET}"
echo -e "  ${CYAN}scion init${RESET}           # Initialize repo (auto-generates AGENTS.md for AI agents)"
echo -e "  ${CYAN}scion status${RESET}         # View working tree changes"
echo -e "  ${CYAN}scion commit -m \"...\"${RESET}  # Record a changeset"
echo -e "  ${CYAN}scion log${RESET}            # View linear timeline"
echo ""
echo -e "${GREEN}${BOLD}OpenScion is ready to use!${RESET}"
