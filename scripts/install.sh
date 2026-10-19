#!/usr/bin/env bash
set -e

echo -e "\033[1;36m[Aether] Building binaries for darwin/arm64...\033[0m"
mkdir -p bin

go build -ldflags="-s -w" -o bin/aether-server ./cmd/aether-server
go build -ldflags="-s -w" -o bin/scn ./cmd/scn
go build -ldflags="-s -w" -o bin/scnd ./cmd/scnd

chmod +x bin/aether-server bin/scn bin/scnd

echo -e "\033[1;32m✔ Successfully compiled all binaries to ./bin:\033[0m"
ls -lh bin/

echo ""
echo -e "\033[33mTo use 'scn' globally, add to your shell profile (~/.zshrc):\033[0m"
echo -e "  export PATH=\"$(pwd)/bin:\$PATH\""
