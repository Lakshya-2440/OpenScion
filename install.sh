#!/usr/bin/env bash
# OpenScion One-Line Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/Lakshya-2440/OpenScion/main/install.sh | bash

set -e

REPO="Lakshya-2440/OpenScion"
BINARY_NAME="scion"
ALIAS_NAME="scn"
VERSION="${OPENSCION_VERSION:-latest}"
CONNECT_TIMEOUT="${OPENSCION_CONNECT_TIMEOUT:-5}"
DOWNLOAD_TIMEOUT="${OPENSCION_DOWNLOAD_TIMEOUT:-30}"

BOLD="\033[1m"
GREEN="\033[32m"
CYAN="\033[36m"
YELLOW="\033[33m"
RED="\033[31m"
RESET="\033[0m"

run_with_progress() {
  label="$1"
  shift
  log_file="$TEMP_DIR/install-step.log"
  "$@" >"$log_file" 2>&1 &
  command_pid=$!
  progress=0
  while kill -0 "$command_pid" 2>/dev/null; do
    filled=$((progress / 5))
    empty=$((20 - filled))
    printf "\r%s [" "$label"
    printf "%0.s#" $(seq 1 "$filled")
    printf "%0.s-" $(seq 1 "$empty")
    printf "] %3d%%" "$progress"
    progress=$((progress + 2))
    [ "$progress" -gt 98 ] && progress=0
    sleep 0.1
  done
  set +e
  wait "$command_pid"
  command_status=$?
  set -e
  if [ "$command_status" -eq 0 ]; then
    printf "\r%s [####################] 100%%\n" "$label"
  else
    printf "\r%s [failed]\n" "$label"
    cat "$log_file" >&2
  fi
  return "$command_status"
}

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

# 2. Choose installation scope
SCOPE="${OPENSCION_SCOPE:-}"
if [ -z "$SCOPE" ] && { [ -t 0 ] || [ -r /dev/tty ]; }; then
  echo ""
  echo "Where should OpenScion be installed?"
  echo "  1) Global (available from every repository)"
  echo "  2) Current repository only (installed under .openscion/bin)"
  while :; do
    printf "Choose [1/2]: "
    if [ -r /dev/tty ]; then
      read -r choice </dev/tty
    else
      read -r choice
    fi
    case "$choice" in
      1) SCOPE="global"; break ;;
      2) SCOPE="repository"; break ;;
      *) echo "Please choose 1 or 2." ;;
    esac
  done
fi
SCOPE="${SCOPE:-global}"
case "$SCOPE" in
  global)
    # Prefer a system-wide location when writable, otherwise use the user bin.
    if [ -w "/usr/local/bin" ]; then
      INSTALL_DIR="/usr/local/bin"
    else
      INSTALL_DIR="$HOME/.local/bin"
    fi
    ;;
  repository|current|local)
    SCOPE="repository"
    INSTALL_DIR="$PWD/.openscion/bin"
    ;;
  *)
    echo -e "${RED}Error: OPENSCION_SCOPE must be global or repository${RESET}"
    exit 1
    ;;
esac
mkdir -p "$INSTALL_DIR"
echo -e "Install scope: ${CYAN}${SCOPE}${RESET}"

if [ "$SCOPE" = "global" ]; then
  SCOPE_LABEL="Installing globally"
else
  SCOPE_LABEL="Installing for the current repository"
fi

TEMP_DIR=$(mktemp -d)
cleanup() {
  rm -rf "$TEMP_DIR"
}
trap cleanup EXIT

INSTALLED=0

# 3. Attempt downloading prebuilt release from GitHub
if [ "$VERSION" = "latest" ]; then
  RELEASE_URL="https://github.com/${REPO}/releases/latest/download/scion-${TARGET}.tar.gz"
else
  RELEASE_URL="https://github.com/${REPO}/releases/download/${VERSION}/scion-${TARGET}.tar.gz"
fi
echo -e "Attempting prebuilt ${VERSION} release download..."

if run_with_progress "$SCOPE_LABEL (downloading release)" curl --connect-timeout "$CONNECT_TIMEOUT" --max-time "$DOWNLOAD_TIMEOUT" -sSfL "$RELEASE_URL" -o "$TEMP_DIR/scion.tar.gz"; then
  tar -xzf "$TEMP_DIR/scion.tar.gz" -C "$TEMP_DIR"
  cp "$TEMP_DIR/scion" "$INSTALL_DIR/$BINARY_NAME"
  chmod +x "$INSTALL_DIR/$BINARY_NAME"
  ln -sf "$INSTALL_DIR/$BINARY_NAME" "$INSTALL_DIR/$ALIAS_NAME"
  INSTALLED=1
  echo -e "${GREEN}✓ Downloaded and installed release binary.${RESET}"
fi

# 4. Fallback: Build from source if Go is present
if [ "$INSTALLED" -eq 0 ]; then
  echo -e "${YELLOW}Prebuilt binary unavailable or timed out after ${DOWNLOAD_TIMEOUT}s; falling back to source compilation.${RESET}"
  if command -v go >/dev/null 2>&1; then
    echo -e "Found Go $(go version | awk '{print $3}'). Compiling from source..."
    run_with_progress "$SCOPE_LABEL (cloning source)" git clone --depth 1 --quiet "https://github.com/${REPO}.git" "$TEMP_DIR/src" || {
      # If git clone fails due to private auth, check if current directory is the repo
      if [ -f "./cmd/scion/main.go" ]; then
        cp -r . "$TEMP_DIR/src"
      else
        echo -e "${RED}Failed to clone repository. If repository is private, ensure your SSH key or git credentials are configured.${RESET}"
        exit 1
      fi
    }
    run_with_progress "$SCOPE_LABEL (building scion)" bash -c "cd \"\$1\" && go build -ldflags='-s -w' -o \"\$2\" ./cmd/scion" _ "$TEMP_DIR/src" "$TEMP_DIR/scion"
    cp "$TEMP_DIR/scion" "$INSTALL_DIR/$BINARY_NAME"
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    if [ -d "$TEMP_DIR/src/cmd/scn" ]; then
      run_with_progress "$SCOPE_LABEL (building scn)" bash -c "cd \"\$1\" && go build -ldflags='-s -w' -o \"\$2\" ./cmd/scn" _ "$TEMP_DIR/src" "$TEMP_DIR/scn"
      cp "$TEMP_DIR/scn" "$INSTALL_DIR/$ALIAS_NAME"
      chmod +x "$INSTALL_DIR/$ALIAS_NAME"
    else
      ln -sf "$INSTALL_DIR/$BINARY_NAME" "$INSTALL_DIR/$ALIAS_NAME"
    fi
    if [ -d "$TEMP_DIR/src/cmd/aether-server" ]; then
      run_with_progress "$SCOPE_LABEL (building aether-server)" bash -c "cd \"\$1\" && go build -ldflags='-s -w' -o \"\$2\" ./cmd/aether-server" _ "$TEMP_DIR/src" "$TEMP_DIR/aether-server"
      cp "$TEMP_DIR/aether-server" "$INSTALL_DIR/aether-server"
      chmod +x "$INSTALL_DIR/aether-server"
    fi
    INSTALLED=1
    echo -e "${GREEN}✓ Successfully compiled and installed OpenScion (scion, scn, aether-server).${RESET}"
  else
    echo -e "${RED}Error: Neither prebuilt binary nor Go compiler found.${RESET}"
    echo -e "Please install Go (https://go.dev) or download release from https://github.com/${REPO}/releases"
    exit 1
  fi
fi

# Ad-hoc codesign on macOS to prevent SIGKILL 137
if [ "$PLATFORM" = "darwin" ] && command -v codesign >/dev/null 2>&1; then
  run_with_progress "$SCOPE_LABEL (signing binary)" codesign -s - -f "$INSTALL_DIR/$BINARY_NAME" || true
  [ -f "$INSTALL_DIR/aether-server" ] && codesign -s - -f "$INSTALL_DIR/aether-server" >/dev/null 2>&1 || true
fi

# 5. Check PATH and configure shell profile if needed
echo ""
echo -e "${GREEN}${BOLD}✓ OpenScion installed to: ${INSTALL_DIR}/${BINARY_NAME}${RESET}"
echo -e "${GREEN}${BOLD}✓ Alias linked: ${INSTALL_DIR}/${ALIAS_NAME}${RESET}"

PATH_NEEDS_UPDATE=1
case ":$PATH:" in
  *":$INSTALL_DIR:"*) PATH_NEEDS_UPDATE=0 ;;
esac

if [ "$SCOPE" = "global" ] && [ "$PATH_NEEDS_UPDATE" -eq 1 ]; then
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
if [ "$SCOPE" = "global" ] && [ -d "$CLAUDE_DIR" ]; then
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
  echo -e "${GREEN}${BOLD}✓ Auto-configured AI coding agents to use OpenScion (scn).${RESET}"
fi

echo ""
echo -e "${BOLD}Get Started:${RESET}"
if [ "$SCOPE" = "repository" ]; then
  echo -e "  ${CYAN}${INSTALL_DIR}/${BINARY_NAME} init${RESET}  # Initialize this repository"
  echo -e "  ${YELLOW}Add ${INSTALL_DIR} to PATH if you want to run scion directly.${RESET}"
else
  echo -e "  ${CYAN}scion init${RESET}           # Initialize repo (auto-generates AGENTS.md for AI agents)"
fi
echo -e "  ${CYAN}scion status${RESET}         # View working tree changes"
echo -e "  ${CYAN}scion commit -m \"...\"${RESET}  # Record a changeset"
echo -e "  ${CYAN}scion log${RESET}            # View linear timeline"
echo ""
echo -e "${GREEN}${BOLD}OpenScion is ready to use!${RESET}"
