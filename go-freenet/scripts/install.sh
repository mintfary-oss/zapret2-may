#!/usr/bin/env bash
# FreeNet installation script for Linux (systemd).
# Run as root:  sudo bash install.sh
#
# What it does:
#   1. Builds the freenet binary from source (requires Go 1.22+)
#   2. Installs the binary to /usr/local/bin/freenet
#   3. Creates the freenet system user
#   4. Writes a default config to /etc/freenet/config.yaml
#   5. Installs and enables the systemd service
#   6. Optionally sets up iptables transparent proxy rules

set -euo pipefail

INSTALL_BIN="/usr/local/bin/freenet"
CONFIG_DIR="/etc/freenet"
DATA_DIR="/var/lib/freenet"
SERVICE_FILE="/etc/systemd/system/freenet.service"
SERVICE_UNIT="$(dirname "$0")/../init.d/systemd/freenet.service"

# ---- colour helpers ----
GREEN="\033[0;32m"; YELLOW="\033[1;33m"; RED="\033[0;31m"; RESET="\033[0m"
info()  { echo -e "${GREEN}[+]${RESET} $*"; }
warn()  { echo -e "${YELLOW}[!]${RESET} $*"; }
error() { echo -e "${RED}[✗]${RESET} $*"; exit 1; }

# ---- root check ----
[ "$(id -u)" -eq 0 ] || error "please run as root: sudo bash $0"

# ---- build ----
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR/.."

if ! command -v go &>/dev/null; then
  error "Go is not installed. Install from https://go.dev/dl/"
fi
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
info "building freenet with Go $GO_VERSION"

(cd "$PROJECT_DIR" && \
  CGO_ENABLED=0 go build -ldflags="-s -w" -o /tmp/freenet ./cmd/freenet)

info "installing binary → $INSTALL_BIN"
install -m 755 /tmp/freenet "$INSTALL_BIN"
rm -f /tmp/freenet

# ---- user ----
if ! id -u freenet &>/dev/null; then
  info "creating system user 'freenet'"
  useradd -r -s /sbin/nologin -d "$DATA_DIR" freenet
fi

# ---- directories ----
install -d -m 755 -o freenet -g freenet "$CONFIG_DIR" "$DATA_DIR"

# ---- default config ----
if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
  info "writing default config → $CONFIG_DIR/config.yaml"
  cat > "$CONFIG_DIR/config.yaml" <<'EOF'
proxy:
  listen_addr: "127.0.0.1:1080"
  transparent_addr: ""   # set to "127.0.0.1:1090" to enable transparent proxy

bypass:
  strategy: "auto"    # auto | split | disorder | none
  split_pos: 2        # byte offset within TLS ClientHello to split
  fake_ttl: 8
  disorder_frag: false

hostlist:
  enabled: false
  path: "/var/lib/freenet/domains.lst"
  auto_update: true
  url: "https://antifilter.download/list/domains.lst"
EOF
  chown freenet:freenet "$CONFIG_DIR/config.yaml"
fi

# ---- systemd service ----
info "installing systemd service"
if [ -f "$SERVICE_UNIT" ]; then
  install -m 644 "$SERVICE_UNIT" "$SERVICE_FILE"
else
  # Write inline if the source file is missing (standalone script mode).
  cat > "$SERVICE_FILE" <<'UNIT'
[Unit]
Description=FreeNet — DPI bypass (Russia / RKN / TSPU)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=freenet
Group=freenet
ExecStart=/usr/local/bin/freenet -config /etc/freenet/config.yaml -web 127.0.0.1:8080
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=/etc/freenet /var/lib/freenet
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
UNIT
fi

systemctl daemon-reload
systemctl enable freenet
systemctl restart freenet

info "freenet is running!"
echo ""
echo -e "  Web UI  → ${GREEN}http://127.0.0.1:8080${RESET}"
echo -e "  SOCKS5  → ${GREEN}127.0.0.1:1080${RESET}"
echo -e "  Logs    → journalctl -u freenet -f"
echo ""

# ---- optional transparent proxy ----
read -rp "$(echo -e "${YELLOW}[?]${RESET} Set up iptables transparent proxy? (y/N) ")" ans
if [[ "${ans,,}" == "y" ]]; then
  bash "$(dirname "$0")/setup-transparent.sh"
fi
