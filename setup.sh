#!/usr/bin/env bash
# OpenScion 1-Command Setup Script
# Usage: ./setup.sh [--server] [--port 8080]

set -e

BOLD="\033[1m"
GREEN="\033[32m"
CYAN="\033[36m"
YELLOW="\033[33m"
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
echo -e "${BOLD}OpenScion 1-Command Setup & Environment Provisioner${RESET}"
echo "--------------------------------------------------------"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 1. Build CLI and Server
echo -e "📦 ${BOLD}Building OpenScion binaries...${RESET}"
mkdir -p bin
go build -ldflags="-s -w" -o bin/scion ./cmd/scion
go build -ldflags="-s -w" -o bin/scn ./cmd/scn
go build -ldflags="-s -w" -o bin/aether-server ./cmd/aether-server
which codesign >/dev/null 2>&1 && codesign -s - -f bin/scion bin/scn bin/aether-server 2>/dev/null || true
echo -e "${GREEN}✔ Built bin/scion, bin/scn, and bin/aether-server${RESET}"

# 2. Install to user bin
INSTALL_DIR="$HOME/.local/bin"
mkdir -p "$INSTALL_DIR"
cp bin/scion "$INSTALL_DIR/scion"
cp bin/scn "$INSTALL_DIR/scn"
cp bin/aether-server "$INSTALL_DIR/aether-server"
which codesign >/dev/null 2>&1 && codesign -s - -f "$INSTALL_DIR/scion" "$INSTALL_DIR/scn" "$INSTALL_DIR/aether-server" 2>/dev/null || true
echo -e "${GREEN}✔ Installed globally to $INSTALL_DIR${RESET}"

# 3. Check PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  echo -e "${YELLOW}Notice: $INSTALL_DIR is not in your PATH.${RESET}"
  echo -e "Add this to your ~/.zshrc or ~/.bashrc:"
  echo -e "  export PATH=\"\$HOME/.local/bin:\$PATH\""
  export PATH="$HOME/.local/bin:$PATH"
fi

echo ""
echo -e "${GREEN}${BOLD}✔ OpenScion Setup Complete!${RESET}"
echo "--------------------------------------------------------"
echo "Quick Commands:"
echo -e "  ${CYAN}scion init${RESET}                    Initialize a local repository"
echo -e "  ${CYAN}scion status${RESET}                  Check working tree status"
echo -e "  ${CYAN}scion commit -m \"...\"${RESET}        Commit changes (with --agent model)"
echo -e "  ${CYAN}scion serve --port 8080${RESET}       Start team server + auto-configured SQLite DB"
echo -e "  ${CYAN}scion clone <url> <dest>${RESET}      Clone repository (Git or OpenScion remote)"
echo "--------------------------------------------------------"

# 4. Optional launch server
if [[ "$1" == "--server" || "$1" == "server" ]]; then
  PORT="${2:-8080}"
  echo -e "🚀 Starting OpenScion server on port $PORT..."
  exec scion serve --port "$PORT"
fi
