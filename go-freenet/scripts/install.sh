#!/usr/bin/env bash
# FreeNet — Linux one-command installer.
#
# Downloads the latest pre-built binary from GitHub Releases, installs it as
# a systemd service, and starts it immediately.  No Go toolchain required.
#
# One-liner:
#   curl -fsSL https://github.com/mintfary-oss/zapret2-may/releases/latest/download/install.sh | sudo bash
#
# Or with a local clone:
#   sudo bash go-freenet/scripts/install.sh
#
# Supports: Linux amd64, arm64, ARMv7 (Raspberry Pi, routers)

set -euo pipefail

# ── Configuration ─────────────────────────────────────────────────────────────
GITHUB_REPO="mintfary-oss/zapret2-may"
INSTALL_BIN="/usr/local/bin/freenet"
CONFIG_DIR="/etc/freenet"
DATA_DIR="/var/lib/freenet"
SERVICE_FILE="/etc/systemd/system/freenet.service"
WEB_ADDR="127.0.0.1:8080"
SOCKS_ADDR="127.0.0.1:1080"

# ── Colour helpers ────────────────────────────────────────────────────────────
G="\033[0;32m" Y="\033[1;33m" R="\033[0;31m" B="\033[1;34m" N="\033[0m"
info()    { echo -e "${G}[+]${N} $*"; }
warn()    { echo -e "${Y}[!]${N} $*"; }
error()   { echo -e "${R}[✗]${N} $*" >&2; exit 1; }
step()    { echo -e "${B}━━ $* ${N}"; }

# ── Checks ────────────────────────────────────────────────────────────────────
[ "$(id -u)" -eq 0 ] || error "Run as root:  sudo bash $0"
command -v curl  >/dev/null 2>&1 || error "curl is required (apt install curl)"
command -v systemctl >/dev/null 2>&1 || error "systemd not found — install manually"

# ── Detect architecture ────────────────────────────────────────────────────────
step "Detecting system architecture"
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)           BIN="freenet-linux-amd64" ;;
  aarch64|arm64)    BIN="freenet-linux-arm64" ;;
  armv7l|armv7)     BIN="freenet-linux-armv7" ;;
  *)                error "Unsupported architecture: $ARCH. Build from source: go build ./cmd/freenet" ;;
esac
info "Architecture: $ARCH → $BIN"

# ── Download ──────────────────────────────────────────────────────────────────
step "Downloading latest FreeNet binary"
URL="https://github.com/${GITHUB_REPO}/releases/latest/download/${BIN}"
info "URL: $URL"
TMP=$(mktemp)
curl -fsSL --progress-bar -o "$TMP" "$URL"
chmod +x "$TMP"

# Quick sanity check.
"$TMP" -version 2>/dev/null || true

# ── Install binary ────────────────────────────────────────────────────────────
step "Installing binary"
install -m 755 "$TMP" "$INSTALL_BIN"
rm -f "$TMP"
info "Installed → $INSTALL_BIN"

# ── Create system user ────────────────────────────────────────────────────────
step "Setting up system user"
if ! id -u freenet &>/dev/null 2>&1; then
  useradd -r -s /sbin/nologin -d "$DATA_DIR" freenet
  info "Created user 'freenet'"
else
  info "User 'freenet' already exists"
fi

# ── Directories ───────────────────────────────────────────────────────────────
install -d -m 755 -o freenet -g freenet "$CONFIG_DIR" "$DATA_DIR"

# ── Default config ────────────────────────────────────────────────────────────
step "Writing configuration"
if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
  cat > "$CONFIG_DIR/config.yaml" <<YAML
proxy:
  listen_addr: "${SOCKS_ADDR}"
  transparent_addr: ""   # "127.0.0.1:1090" to enable transparent proxy

bypass:
  strategy: "auto"    # auto | split | disorder | fake | tlsrec | combined | none
  split_pos: 2
  fake_ttl: 8

hostlist:
  enabled: false
  path: "${DATA_DIR}/domains.lst"
  auto_update: true
  url: "https://antifilter.download/list/domains.lst"
YAML
  chown freenet:freenet "$CONFIG_DIR/config.yaml"
  info "Config written → $CONFIG_DIR/config.yaml"
else
  info "Config already exists, skipping"
fi

# ── systemd service ───────────────────────────────────────────────────────────
step "Installing systemd service"
cat > "$SERVICE_FILE" <<UNIT
[Unit]
Description=FreeNet — DPI bypass (Russia / RKN / TSPU)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=freenet
Group=freenet
ExecStart=${INSTALL_BIN} -config ${CONFIG_DIR}/config.yaml -web ${WEB_ADDR}
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=${CONFIG_DIR} ${DATA_DIR}
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable freenet
systemctl restart freenet

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo -e "  ${G}✓ FreeNet установлен и запущен!${N}"
echo ""
echo -e "  Веб-интерфейс  → ${B}http://${WEB_ADDR}${N}"
echo -e "  SOCKS5 прокси  → ${B}${SOCKS_ADDR}${N}"
echo -e "  Логи           → journalctl -u freenet -f"
echo -e "  Остановить     → systemctl stop freenet"
echo -e "  Статус         → systemctl status freenet"
echo ""

# ── Optional: transparent proxy ───────────────────────────────────────────────
if [[ -t 0 ]]; then  # only prompt when running interactively
  read -rp "$(echo -e "${Y}[?]${N} Настроить iptables прозрачный прокси? (y/N) ")" ans
  if [[ "${ans,,}" == "y" ]]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    bash "$SCRIPT_DIR/setup-transparent.sh" 2>/dev/null || \
      warn "setup-transparent.sh not found — run manually from the source tree"
  fi
fi
