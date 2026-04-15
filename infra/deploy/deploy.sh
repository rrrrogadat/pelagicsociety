#!/usr/bin/env bash
# Pelagic Society deployment.
#
# Builds the single self-contained Go binary (templates + static CSS/assets
# embedded), rsyncs deploy files to the server, swaps the binary behind a
# maintenance page, restarts the systemd unit, and rolls back automatically if
# the new binary fails its health check.
#
# Config via environment (or .env.deploy at repo root):
#   Required:
#     PELAGIC_SERVER         SSH target (e.g. ubuntu@13.59.65.92)
#     PELAGIC_SSH_KEY        path to SSH private key (e.g. ~/.ssh/pelagic_deploy)
#   Optional:
#     PELAGIC_DEPLOY_PATH    remote deploy path (default: /opt/pelagicsociety)
#     PELAGIC_DOMAIN         public domain (default: pelagicsociety.com)
#     PELAGIC_SKIP_BUILD     if "1", skip `make release` (use existing bin/pelagicsociety)
#
# Usage:
#   source .env.deploy && ./infra/deploy/deploy.sh
#   PELAGIC_SKIP_BUILD=1 ./infra/deploy/deploy.sh

set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

if [ -f "${PROJECT_ROOT}/.env.deploy" ]; then
    echo -e "${BLUE}Loading configuration from .env.deploy${NC}"
    set -a
    # shellcheck disable=SC1091
    source "${PROJECT_ROOT}/.env.deploy"
    set +a
fi

SERVER="${PELAGIC_SERVER:-}"
SSH_KEY="${PELAGIC_SSH_KEY:-}"
DEPLOY_PATH="${PELAGIC_DEPLOY_PATH:-/opt/pelagicsociety}"
DOMAIN="${PELAGIC_DOMAIN:-pelagicsociety.com}"
SKIP_BUILD="${PELAGIC_SKIP_BUILD:-0}"

if [ -z "$SERVER" ]; then
    echo -e "${RED}Error: PELAGIC_SERVER is not set${NC}"
    echo "Set it in .env.deploy or export PELAGIC_SERVER=user@host"
    exit 1
fi
if [ -z "$SSH_KEY" ]; then
    echo -e "${RED}Error: PELAGIC_SSH_KEY is not set${NC}"; exit 1
fi
SSH_KEY="${SSH_KEY/#\~/$HOME}"
if [ ! -f "$SSH_KEY" ]; then
    echo -e "${RED}Error: SSH key not found: $SSH_KEY${NC}"; exit 1
fi

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Pelagic Society deployment${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "${YELLOW}  Server:  $SERVER${NC}"
echo -e "${YELLOW}  Path:    $DEPLOY_PATH${NC}"
echo -e "${YELLOW}  Domain:  $DOMAIN${NC}"
echo -e "${YELLOW}  SSH key: $SSH_KEY${NC}"
echo ""

DEPLOY_START=$SECONDS

# --- SSH connection multiplexing ---
SSH_CONTROL="/tmp/pelagic-deploy-$$"
SSH_MUX="-o ControlMaster=auto -o ControlPath=$SSH_CONTROL -o ControlPersist=120 -o StrictHostKeyChecking=accept-new"
ssh -i "$SSH_KEY" $SSH_MUX -fNM "$SERVER"
cleanup() {
    ssh -i "$SSH_KEY" -o ControlPath="$SSH_CONTROL" -O exit "$SERVER" 2>/dev/null || true
    rm -f "$SSH_CONTROL"
}
trap cleanup EXIT

cd "$PROJECT_ROOT"

# --- [1/4] Build release ---
echo -e "${YELLOW}[1/4] Building release${NC}"
STEP_START=$SECONDS
if [ "$SKIP_BUILD" = "1" ]; then
    echo "  PELAGIC_SKIP_BUILD=1 — using existing bin/pelagicsociety"
    if [ ! -f bin/pelagicsociety ]; then
        echo -e "${RED}✗ bin/pelagicsociety missing${NC}"; exit 1
    fi
else
    make release
fi
BIN_SIZE=$(du -h bin/pelagicsociety | cut -f1)
echo -e "${GREEN}✓ bin/pelagicsociety built ($BIN_SIZE, $((SECONDS - STEP_START))s)${NC}"
echo ""

# --- [2/4] Stage files and rsync to server ---
echo -e "${YELLOW}[2/4] Syncing deploy files${NC}"
STEP_START=$SECONDS
TEMP_DIR=$(mktemp -d)
mkdir -p "$TEMP_DIR/deploy/bin" "$TEMP_DIR/deploy/systemd" "$TEMP_DIR/deploy/nginx" "$TEMP_DIR/deploy/web"

cp bin/pelagicsociety                              "$TEMP_DIR/deploy/bin/"
cp infra/systemd/pelagicsociety.service            "$TEMP_DIR/deploy/systemd/"
cp infra/nginx/pelagicsociety.conf                 "$TEMP_DIR/deploy/nginx/"
cp infra/web/maintenance.html                      "$TEMP_DIR/deploy/web/"

if ssh -i "$SSH_KEY" -o ControlPath="$SSH_CONTROL" "$SERVER" \
     "sudo mkdir -p $DEPLOY_PATH/.deploy-cache && sudo chown -R \$USER $DEPLOY_PATH/.deploy-cache && command -v rsync" > /dev/null 2>&1; then
    echo "  using rsync (delta transfer)"
    rsync -rlz --checksum \
        -e "ssh -i $SSH_KEY -o ControlPath=$SSH_CONTROL" \
        "$TEMP_DIR/deploy/" "$SERVER:$DEPLOY_PATH/.deploy-cache/"
else
    echo "  rsync not available — falling back to scp + tar"
    tar -czf "$TEMP_DIR/pelagic-deploy.tar.gz" -C "$TEMP_DIR/deploy" .
    scp -C -i "$SSH_KEY" -o ControlPath="$SSH_CONTROL" \
        "$TEMP_DIR/pelagic-deploy.tar.gz" "$SERVER:/tmp/"
    ssh -i "$SSH_KEY" -o ControlPath="$SSH_CONTROL" "$SERVER" \
        "cd $DEPLOY_PATH/.deploy-cache && tar -xzf /tmp/pelagic-deploy.tar.gz && rm /tmp/pelagic-deploy.tar.gz"
fi
rm -rf "$TEMP_DIR"
echo -e "${GREEN}✓ files synced ($((SECONDS - STEP_START))s)${NC}"
echo ""

# --- [3/4] Remote swap + restart with auto-rollback ---
echo -e "${YELLOW}[3/4] Deploying on server${NC}"
STEP_START=$SECONDS

ssh -i "$SSH_KEY" -o ControlPath="$SSH_CONTROL" "$SERVER" bash <<REMOTE
set -euo pipefail
DEPLOY_PATH="$DEPLOY_PATH"

# --- ensure data dir exists (for pre-existing installs) ---
sudo mkdir -p "\$DEPLOY_PATH/data"
sudo chown pelagicsociety:pelagicsociety "\$DEPLOY_PATH/data"
sudo chmod 750 "\$DEPLOY_PATH/data"

# --- ensure env file exists so EnvironmentFile=- is happy even if empty ---
sudo mkdir -p /etc/pelagicsociety
if [ ! -f /etc/pelagicsociety/env ]; then
    sudo touch /etc/pelagicsociety/env
    sudo chown root:pelagicsociety /etc/pelagicsociety/env
    sudo chmod 640 /etc/pelagicsociety/env
fi

# --- maintenance mode on ---
sudo mkdir -p /var/www/pelagicsociety/maintenance
sudo cp "\$DEPLOY_PATH/.deploy-cache/web/maintenance.html" /var/www/pelagicsociety/maintenance/index.html
sudo chown -R www-data:www-data /var/www/pelagicsociety
sudo touch /var/www/pelagicsociety/maintenance/.enabled
echo "✓ maintenance mode on"

# --- verify new binary present ---
if [ ! -f "\$DEPLOY_PATH/.deploy-cache/bin/pelagicsociety" ]; then
    echo "ERROR: new binary missing from cache"
    sudo rm -f /var/www/pelagicsociety/maintenance/.enabled
    exit 1
fi

# --- stop service ---
sudo systemctl stop pelagicsociety 2>/dev/null || true
echo "✓ service stopped"

# --- backup current binary ---
if [ -f "\$DEPLOY_PATH/bin/pelagicsociety" ]; then
    sudo cp "\$DEPLOY_PATH/bin/pelagicsociety" "\$DEPLOY_PATH/bin/pelagicsociety.backup"
fi

# --- swap binary in place ---
sudo install -o pelagicsociety -g pelagicsociety -m 0755 \
    "\$DEPLOY_PATH/.deploy-cache/bin/pelagicsociety" "\$DEPLOY_PATH/bin/pelagicsociety"
echo "✓ binary swapped"

# --- systemd unit (reload if changed) ---
if ! sudo cmp -s "\$DEPLOY_PATH/.deploy-cache/systemd/pelagicsociety.service" "/etc/systemd/system/pelagicsociety.service" 2>/dev/null; then
    sudo cp "\$DEPLOY_PATH/.deploy-cache/systemd/pelagicsociety.service" "/etc/systemd/system/pelagicsociety.service"
    sudo systemctl daemon-reload
    sudo systemctl enable pelagicsociety
    echo "✓ systemd unit updated"
fi

# --- nginx config (reload if changed and valid) ---
if ! sudo cmp -s "\$DEPLOY_PATH/.deploy-cache/nginx/pelagicsociety.conf" "/etc/nginx/sites-available/pelagicsociety" 2>/dev/null; then
    sudo cp "\$DEPLOY_PATH/.deploy-cache/nginx/pelagicsociety.conf" "/etc/nginx/sites-available/pelagicsociety"
    sudo ln -sf /etc/nginx/sites-available/pelagicsociety /etc/nginx/sites-enabled/pelagicsociety
    sudo rm -f /etc/nginx/sites-enabled/default
    if sudo nginx -t 2>&1 | grep -q "successful"; then
        sudo systemctl reload nginx
        echo "✓ nginx reloaded"
    else
        echo "⚠ nginx config test failed — keeping previous config"
        sudo nginx -t
    fi
fi

# --- start service ---
sudo systemctl start pelagicsociety
echo "✓ service started"

# --- health check with auto-rollback ---
APP_READY=false
for i in \$(seq 1 20); do
    if curl -sf http://127.0.0.1:8080/healthz > /dev/null; then
        APP_READY=true
        break
    fi
    if [ \$i -ge 5 ] && ! sudo systemctl is-active --quiet pelagicsociety; then
        echo "ERROR: pelagicsociety crashed — rolling back"
        if [ -f "\$DEPLOY_PATH/bin/pelagicsociety.backup" ]; then
            sudo install -o pelagicsociety -g pelagicsociety -m 0755 \
                "\$DEPLOY_PATH/bin/pelagicsociety.backup" "\$DEPLOY_PATH/bin/pelagicsociety"
            sudo systemctl start pelagicsociety
            sleep 2
            if sudo systemctl is-active --quiet pelagicsociety; then
                echo "✓ rollback successful"
                APP_READY=true
            fi
        fi
        break
    fi
    sleep 1
done

if [ "\$APP_READY" = false ]; then
    echo "ERROR: health check failed; leaving maintenance mode on for manual inspection"
    sudo journalctl -u pelagicsociety -n 30 --no-pager
    exit 1
fi

# --- maintenance mode off ---
sudo rm -f /var/www/pelagicsociety/maintenance/.enabled
echo "✓ live"
echo ""
echo "---- pelagicsociety status ----"
sudo systemctl status pelagicsociety --no-pager -l | head -n 10
REMOTE

echo -e "${GREEN}✓ remote deploy complete ($((SECONDS - STEP_START))s)${NC}"
echo ""

# --- [4/4] External health check ---
echo -e "${YELLOW}[4/4] Verifying via https://$DOMAIN${NC}"
if curl -fsS "https://$DOMAIN/healthz" > /dev/null; then
    echo -e "${GREEN}✓ https://$DOMAIN/healthz OK${NC}"
else
    echo -e "${YELLOW}⚠ could not verify via public URL — may be DNS/cert still propagating${NC}"
fi

DEPLOY_TIME=$((SECONDS - DEPLOY_START))
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Deploy complete in ${DEPLOY_TIME}s${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "Logs:   ssh -i $SSH_KEY $SERVER 'sudo journalctl -u pelagicsociety -f'"
echo "Status: ssh -i $SSH_KEY $SERVER 'sudo systemctl status pelagicsociety'"
