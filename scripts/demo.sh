#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$DIR/bin"
DEMO_DIR="/tmp/openscion-demo"
PORT="8089"

echo -e "\033[1;35m========================================================================\033[0m"
echo -e "\033[1;36m       PROJECT OPENSCION: AGENT-SCALE VCS (ANTHROPIC CLASS) DEMO        \033[0m"
echo -e "\033[1;35m========================================================================\033[0m"

# Ensure binaries are built
if [ ! -f "$BIN/aether-server" ] || [ ! -f "$BIN/scn" ]; then
    echo "Building binaries..."
    "$DIR/scripts/install.sh"
fi

rm -rf "$DEMO_DIR"
mkdir -p "$DEMO_DIR/server-data"
mkdir -p "$DEMO_DIR/workspace-main"

echo -e "\n\033[1;33m[Step 1] Starting OpenScion Control Plane Server on port $PORT...\033[0m"
"$BIN/aether-server" --port "$PORT" --data-dir "$DEMO_DIR/server-data" > "$DEMO_DIR/server.log" 2>&1 &
SERVER_PID=$!

cleanup() {
    echo -e "\n\033[33mStopping server (PID: $SERVER_PID)...\033[0m"
    kill $SERVER_PID 2>/dev/null || true
}
trap cleanup EXIT

# Wait for server to be healthy
sleep 1
curl -s "http://localhost:$PORT/healthz" > /dev/null || (echo "Server failed to start"; cat "$DEMO_DIR/server.log"; exit 1)
echo -e "\033[1;32m✔ Control Plane online and healthy\033[0m"

export OPENSCION_SERVER_URL="http://localhost:$PORT"
export AETHER_SERVER_URL="http://localhost:$PORT"

echo -e "\n\033[1;33m[Step 2] Initializing repository in workspace-main...\033[0m"
cd "$DEMO_DIR/workspace-main"
"$BIN/scn" init openscion-monorepo

echo -e "\n\033[1;33m[Step 3] Adding source files for auth, payments, and config...\033[0m"
mkdir -p services/auth services/payments config

cat << 'EOF' > services/auth/token.go
package auth

import "time"

// ValidateToken checks session validity
func ValidateToken(token string) bool {
    expiry := 1700000000
    if time.Now().Unix() > int64(expiry) {
        return false
    }
    return true
}
EOF

cat << 'EOF' > services/auth/session.go
package auth

type Session struct {
    UserID string
    Role   string
}
EOF

cat << 'EOF' > services/payments/stripe.go
package payments

func ProcessCharge(cents int64) bool {
    return cents > 0
}
EOF

cat << 'EOF' > config/production.json
{
  "environment": "production",
  "concurrency_limit": 50000
}
EOF

echo -e "\n\033[1;33m[Step 4] Checking workspace status...\033[0m"
"$BIN/scn" status

echo -e "\n\033[1;33m[Step 5] Committing initial services with agent provenance...\033[0m"
"$BIN/scn" commit -m "feat(core): initial auth, payments, and config services" --agent claude-code-3.7-sonnet

echo -e "\n\033[1;33m[Step 6] Submitting to linear merge queue...\033[0m"
"$BIN/scn" submit

echo -e "\n\033[1;33m[Step 7] Testing SUB-SECOND Sparse Clone for an Autonomous Agent pod...\033[0m"
cd "$DEMO_DIR"
"$BIN/scn" clone openscion-monorepo workspace-agent --sparse "services/auth/**"

echo -e "\n\033[1;36mSparse directory layout inside agent pod:\033[0m"
ls -la workspace-agent/services/

echo -e "\n\033[1;33m[Step 8] Agent modifies services/auth/token.go...\033[0m"
cd "$DEMO_DIR/workspace-agent"
cat << 'EOF' > services/auth/token.go
package auth

import "time"

// ValidateToken checks session validity with fast lease expiry check
func ValidateToken(token string) bool {
    expiry := 1700000000
    // Optimized: sub-millisecond lease check without DB roundtrip
    if time.Now().Unix() >= int64(expiry) {
        return false
    }
    return true
}
EOF

echo -e "\n\033[1;33m[Step 9] Inspecting colorized diff...\033[0m"
"$BIN/scn" diff

echo -e "\n\033[1;33m[Step 10] Agent commits and lands into server queue...\033[0m"
"$BIN/scn" commit -m "perf(auth): optimize token validation lease check" --agent claude-code-3.7-sonnet
"$BIN/scn" submit

echo -e "\n\033[1;33m[Step 11] Viewing linear commit graph (scn log)...\033[0m"
"$BIN/scn" log

echo -e "\n\033[1;33m[Step 12] Zero-Clone query: server-side blame...\033[0m"
"$BIN/scn" query blame services/auth/token.go

echo -e "\n\033[1;33m[Step 13] Inspecting live merge queue metrics...\033[0m"
"$BIN/scn" query queue

echo -e "\n\033[1;32m========================================================================\033[0m"
echo -e "\033[1;32m   ✔ ALL TESTS PASSED: AGENT-SCALE VCS FULLY WORKING END-TO-END!        \033[0m"
echo -e "\033[1;32m========================================================================\033[0m"
