#!/bin/sh
# Enables MCP token issuance: deploy bundle v1.5.1-rc.7 + app 1.6.1-rc.11.
# Run from the installed stack directory (where .env and spartaskctl are), after closing meetings:
#   curl -fsSL <raw url of this file> | sh
set -eu
test -f .env
test -f spartaskctl
test -f compose.yml
mcp_tmp=$(mktemp -d)
mcp_release=https://github.com/spartaskai/spartask-deploy/releases/download/v1.5.1-rc.7
mcp_app=1.6.1-rc.11
curl -fL "$mcp_release/spartask-deploy-linux-amd64.tar.gz" -o "$mcp_tmp/spartask-deploy-linux-amd64.tar.gz"
curl -fL "$mcp_release/SHA256SUMS" -o "$mcp_tmp/SHA256SUMS"
(cd "$mcp_tmp" && grep ' spartask-deploy-linux-amd64.tar.gz$' SHA256SUMS | sha256sum -c -)
tar -xzf "$mcp_tmp/spartask-deploy-linux-amd64.tar.gz" -C "$mcp_tmp"
mcp_bundle="$mcp_tmp/spartask-deploy"
# Read-only: stops if speech jobs, open meetings or retired speech nodes would break the new release.
sudo docker compose exec -T db sh -c 'exec psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < "$mcp_bundle/browser-speech-preflight.sql"
mcp_backup="$(pwd)/mcp-backup-$(date +%Y%m%d%H%M%S)"
sudo mkdir -m 700 "$mcp_backup"
sudo cp -p spartaskctl .env .env.example compose.yml "$mcp_backup/"
echo "Yedek: $mcp_backup (geri almak için bu dosyaları geri kopyalayıp: sudo ./spartaskctl restart)"
sudo install -m 755 "$mcp_bundle/spartaskctl" ./spartaskctl
sudo install -m 644 "$mcp_bundle/.env.example" ./.env.example
# update does not replace compose.yml; the new one passes MCP_JWT_PUBLIC_KEY_FILE to the API.
sudo install -m 644 "$mcp_bundle/compose.yml" ./compose.yml
sudo sed -i 's/^MCP_ENABLED=.*/MCP_ENABLED=true/; s/^MCP_TOKEN_ISSUANCE_ENABLED=.*/MCP_TOKEN_ISSUANCE_ENABLED=true/' .env
sudo grep -qx 'MCP_ENABLED=true' .env
sudo grep -qx 'MCP_TOKEN_ISSUANCE_ENABLED=true' .env
# Generates .secrets/mcp/private.pem + public.pem and fills MCP_RESOURCE_URL / MCP_JWT_ISSUER.
sudo ./spartaskctl config
sudo ./spartaskctl config -check
sudo test -s .secrets/mcp/private.pem
sudo test -s .secrets/mcp/public.pem
sudo grep -E '^MCP_(ENABLED|TOKEN_ISSUANCE_ENABLED|RESOURCE_URL|JWT_ISSUER)=' .env
sudo ./spartaskctl update "$mcp_app"
sudo ./spartaskctl status
# 200 + JSON only when MCP is enabled in the running API.
sudo docker compose exec -T api curl -fsS http://localhost:8080/.well-known/oauth-protected-resource/mcp
echo
echo "MCP açık. Web'de MCP bağlantı penceresinden token üretmeyi deneyin."
