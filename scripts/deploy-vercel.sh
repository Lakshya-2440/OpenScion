#!/usr/bin/env bash
set -e

echo -e "\033[1;36m[Vercel CLI] Initiating Antique Web Review Platform Deployment...\033[0m"

# 1. Check if vercel CLI exists
if ! command -v vercel &> /dev/null; then
    echo -e "\033[31mError: vercel CLI not found in PATH.\033[0m"
    echo -e "Install with npm: \033[1;33mnpm install -g vercel\033[0m"
    exit 1
fi

# 2. Check authentication
echo "Checking Vercel authentication status..."
if ! vercel whoami &> /dev/null; then
    if [ -n "$VERCEL_TOKEN" ]; then
        echo -e "\033[32m✔ Using VERCEL_TOKEN from environment\033[0m"
    else
        echo -e "\033[33mNot logged in to Vercel. Opening Vercel login...\033[0m"
        vercel login
    fi
else
    USER_INFO=$(vercel whoami 2>/dev/null || true)
    echo -e "\033[32m✔ Logged in to Vercel: ${USER_INFO}\033[0m"
fi

# 3. Build web app locally
echo -e "\nBuilding production bundle for web dashboard..."
cd web
npm run build

# 4. Deploy to Vercel production
echo -e "\nDeploying to Vercel production..."
if [ -n "$VERCEL_TOKEN" ]; then
    vercel deploy --prod --yes --token="$VERCEL_TOKEN"
else
    vercel deploy --prod --yes
fi

echo -e "\033[1;32m✔ Vercel deployment completed successfully!\033[0m"
