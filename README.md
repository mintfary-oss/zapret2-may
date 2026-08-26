# FreeNet — DPI bypass for Russia / Россия

Cross-platform DPI bypass tool for Russian ISPs (РКН/ТСПУ). Works without root on Android via VpnService API.

## Platforms

| Platform | Status |
|----------|--------|
| Linux (amd64/arm64/MIPS/MIPSle) | Production-ready |
| Windows 10/11 (amd64) — WinDivert kernel bypass | Production-ready |
| Android 8.0+ — VPN without root | Production-ready |
| OpenWrt (ARM/MIPS) | Production-ready |
| Docker | Production-ready |

## Quick start

```bash
# Docker (recommended)
cd go-freenet && docker compose up -d
# → http://localhost:8080  (web UI)
# → 127.0.0.1:1080        (SOCKS5 proxy)

# Linux binary
go build -o freenet ./cmd/freenet && ./freenet -web :8080

# Android
# Download freenet-android.apk from GitHub Releases and install,
# or build: bash go-freenet/scripts/build-release-apk.sh
```

## Bypass strategies

| Strategy | How it works |
|----------|-------------|
| `auto` | Tests all strategies and picks the best one for your ISP |
| `split` | Splits TLS ClientHello at the SNI position into two TCP segments |
| `tlsrec` | Splits into two TLS records |
| `disorder` | Sends segments out of order |
| `fake` | Sends decoy packets with TTL=4/bad-checksum before the real data (Linux) |
| `combined` | Fake + split combined |
| `none` | Plain forwarding (benchmark/debug) |

ECH (Encrypted Client Hello, RFC 9601) is detected automatically and forwarded without modification.

## Telegram bot (remote management)

The Telegram bot lets you control a FreeNet server from your phone.

### Setup

1. Create a bot via [@BotFather](https://t.me/BotFather) → receive a token like `1234567890:ABC...`
2. Start FreeNet with the bot enabled:

```bash
# Via command-line flags
./freenet -telegram-token 1234567890:ABCDEFGxxxxxxxx

# Restrict control to your personal account (get your chat ID from @userinfobot)
./freenet -telegram-token 1234567890:ABCDEFGxxxxxxxx -telegram-chat-id 987654321

# Via config file (freenet.yaml)
telegram:
  token: "1234567890:ABCDEFGxxxxxxxx"
  allowed_chat_id: 987654321   # 0 = any user
```

### Available commands

| Command | Action |
|---------|--------|
| `/status` | Current state: enabled/disabled, strategy, connections |
| `/on` | Enable DPI bypass |
| `/off` | Disable DPI bypass (plain forwarding) |
| `/strategy auto` | Set strategy (auto/split/tlsrec/disorder/fake/combined/none) |
| `/stats` | Connection statistics |
| `/help` | List all commands |

### Security

- Keep your bot token private — it grants full control over the server.
- Use `allowed_chat_id` to restrict access to your personal Telegram account.
- Never commit the token to source control — use environment variables or a secrets manager.

## DNS-over-HTTPS

Built-in DoH resolver on `127.0.0.1:5300` (Cloudflare, Google, Quad9 by default). Protects against ISP DNS poisoning. Enabled by default.

## F-Droid

The F-Droid manifest is in `metadata/com.freenet.vpn.yml`. The app builds cleanly without the gomobile AAR — `FreenetVpnService` falls back to the pure-Kotlin `PacketForwarder` automatically.

## Contributing

See [.github/CONTRIBUTING.md](.github/CONTRIBUTING.md).

## Security

See [.github/SECURITY.md](.github/SECURITY.md).

## License

MIT
