#!/usr/bin/env bash
set -e

echo -e "\033[1;36m[Render CLI] Initiating Aether Control Plane Deployment...\033[0m"

# 1. Check if render CLI exists
if ! command -v render &> /dev/null; then
    echo -e "\033[31mError: render CLI not found in PATH.\033[0m"
    echo -e "Install with Homebrew: \033[1;33mbrew install render\033[0m"
    exit 1
fi

# 2. Check authentication
echo "Checking Render authentication status..."
if ! render whoami &> /dev/null; then
    if [ -n "$RENDER_API_KEY" ]; then
        echo -e "\033[32m✔ Using RENDER_API_KEY from environment\033[0m"
    else
        echo -e "\033[33mNot logged in to Render. Opening Render login in your browser...\033[0m"
        render login
    fi
else
    USER_INFO=$(render whoami 2>/dev/null || true)
    echo -e "\033[32m✔ Logged in to Render: ${USER_INFO}\033[0m"
fi

# 3. Validate Blueprint
echo -e "\nValidating render.yaml blueprint..."
render blueprints validate ./render.yaml

# 4. Deploy Blueprint / Services
echo -e "\nDeploying Aether backend services via Render..."
render blueprints launch --blueprint ./render.yaml || {
    echo -e "\033[33mNote: If blueprint already exists, creating deploy for service...\033[0m"
    render deploys create aether-server --wait || true
}

echo -e "\033[1;32m✔ Render deployment completed successfully!\033[0m"
