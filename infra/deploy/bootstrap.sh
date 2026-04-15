#!/usr/bin/env bash
# One-time server bootstrap. Idempotent — safe to re-run.
#
# Creates the pelagicsociety system user, directory layout, installs nginx +
# certbot, obtains TLS certs, and prepares maintenance page directories.
#
# Run this ONCE against a fresh EC2 instance before using deploy.sh.
#
# Usage:
#   source .env.deploy && ./infra/deploy/bootstrap.sh

set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

if [ -f "${PROJECT_ROOT}/.env.deploy" ]; then
    set -a; source "${PROJECT_ROOT}/.env.deploy"; set +a
fi

SERVER="${PELAGIC_SERVER:-}"
SSH_KEY="${PELAGIC_SSH_KEY:-}"
DEPLOY_PATH="${PELAGIC_DEPLOY_PATH:-/opt/pelagicsociety}"
DOMAIN="${PELAGIC_DOMAIN:-pelagicsociety.com}"
ADMIN_EMAIL="${PELAGIC_ADMIN_EMAIL:-}"

for v in SERVER SSH_KEY ADMIN_EMAIL; do
    if [ -z "${!v}" ]; then
        echo -e "${RED}Error: PELAGIC_${v} not set (needed in .env.deploy)${NC}"; exit 1
    fi
done
SSH_KEY="${SSH_KEY/#\~/$HOME}"

echo -e "${BLUE}Bootstrapping $SERVER for $DOMAIN${NC}"

ssh -i "$SSH_KEY" -o StrictHostKeyChecking=accept-new "$SERVER" bash <<REMOTE
set -euo pipefail
DEPLOY_PATH="$DEPLOY_PATH"
DOMAIN="$DOMAIN"
ADMIN_EMAIL="$ADMIN_EMAIL"

# --- packages ---
sudo apt-get update -y
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y nginx certbot python3-certbot-nginx rsync curl

# --- system user ---
if ! id pelagicsociety &>/dev/null; then
    sudo useradd --system --shell /usr/sbin/nologin --home-dir "\$DEPLOY_PATH" pelagicsociety
    echo "✓ created pelagicsociety user"
fi

# --- directories ---
sudo mkdir -p "\$DEPLOY_PATH/bin" "\$DEPLOY_PATH/.deploy-cache" "\$DEPLOY_PATH/data"
sudo chown -R pelagicsociety:pelagicsociety "\$DEPLOY_PATH"
sudo chmod 750 "\$DEPLOY_PATH/data"
sudo mkdir -p /var/www/pelagicsociety/maintenance
sudo mkdir -p /var/www/certbot
sudo chown -R www-data:www-data /var/www/pelagicsociety /var/www/certbot

# --- env file for secrets (Resend, etc.) ---
sudo mkdir -p /etc/pelagicsociety
if [ ! -f /etc/pelagicsociety/env ]; then
    sudo tee /etc/pelagicsociety/env > /dev/null <<'EOF'
# Pelagic Society runtime config. Loaded by systemd via EnvironmentFile=.
# Mail is sent via AWS SESv2. Credentials resolve in this order:
#   1. IAM instance role (preferred; leave AWS_* vars unset)
#   2. AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY below
# If neither works, the app runs in log-only mode (no email sent).
AWS_REGION=us-east-2
MAIL_FROM=Pelagic Society <no-reply@pelagicsociety.com>
MAIL_REPLY_TO=
CONTACT_TO=
# AWS_ACCESS_KEY_ID=
# AWS_SECRET_ACCESS_KEY=
EOF
    sudo chown root:pelagicsociety /etc/pelagicsociety/env
    sudo chmod 640 /etc/pelagicsociety/env
    echo "✓ /etc/pelagicsociety/env created (edit to add RESEND_API_KEY + CONTACT_TO)"
else
    echo "✓ /etc/pelagicsociety/env already present"
fi

# --- firewall (if ufw active) ---
if sudo ufw status 2>/dev/null | grep -q "Status: active"; then
    sudo ufw allow 'Nginx Full' || true
fi

# --- TLS cert (only if not already present) ---
if [ ! -f "/etc/letsencrypt/live/\$DOMAIN/fullchain.pem" ]; then
    # Temporary minimal server for ACME challenge
    sudo tee /etc/nginx/sites-available/pelagic-bootstrap > /dev/null <<EOF
server {
    listen 80 default_server;
    server_name \$DOMAIN;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 200 'bootstrap'; add_header Content-Type text/plain; }
}
EOF
    sudo ln -sf /etc/nginx/sites-available/pelagic-bootstrap /etc/nginx/sites-enabled/pelagic-bootstrap
    sudo rm -f /etc/nginx/sites-enabled/default
    sudo nginx -t && sudo systemctl reload nginx

    # Only apex domain — www has no DNS record yet. Re-run bootstrap after
    # adding a www A-record to expand the cert.
    CERT_DOMAINS="-d \$DOMAIN"
    if host "www.\$DOMAIN" >/dev/null 2>&1; then
        CERT_DOMAINS="\$CERT_DOMAINS -d www.\$DOMAIN"
    fi
    sudo certbot certonly --webroot -w /var/www/certbot \
        \$CERT_DOMAINS \
        --non-interactive --agree-tos -m "\$ADMIN_EMAIL"

    sudo rm -f /etc/nginx/sites-enabled/pelagic-bootstrap
    echo "✓ TLS cert obtained"
else
    echo "✓ TLS cert already present"
fi

sudo systemctl enable nginx
echo "✓ bootstrap complete"
REMOTE

echo -e "${GREEN}✓ Bootstrap done. Now run: ./infra/deploy/deploy.sh${NC}"
