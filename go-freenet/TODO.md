# Задачи — FreeNet Go

## Текущий статус

| Phase | Название | Версия | Статус |
|-------|----------|--------|--------|
| 1 | Ядро + Docker + Linux | v1.0.5 | ✅ Завершена |
| 2 | Fake packets + nfqueue + TLS record | v1.0.5 | ✅ Завершена |
| 3 | Android APK (gomobile + VpnService) | v1.0.5 | ✅ Завершена |
| 4а | Windows системный трей | v1.1.0 | ✅ Завершена |
| 4б | Windows авто системный прокси | v1.2.0 | ✅ Завершена |
| 4в | DNS-over-HTTPS защита | v1.3.0 | ✅ Завершена |
| 5 | ECH + unit tests (35 тестов) | v1.4.0 | ✅ Завершена |
| 6 | Windows WinDivert (kernel bypass) | v1.5.2 | ✅ Завершена |
| 7 | Android: UDP relay, split tunnel, виджет | v1.6.0 | ✅ Завершена |
| 8 | F-Droid + OpenWrt + качество | v1.7.0 | ✅ Завершена |
| 9 | Telegram бот + Release signing + тесты | v1.8.0 | ✅ Завершена |
| 10 | Test coverage ~70%+ + GitHub files | v1.9.0 | ✅ Завершена |
| 11 | Test coverage ~90%+ | v1.9.1 | ✅ Завершена |
| 12 | Smoke testing + Platform verification | v1.9.1 | ✅ Завершена |
| 13 | Android Go engine fix + full verification | v1.8.1 | ✅ Завершена |

---

## Phase 9 — Telegram бот + Release signing + тесты ✅ ЗАВЕРШЕНА (v1.8.0)

### 9.1 Android release signing ✅

`build.gradle.kts` — `signingConfigs` на env-переменных:
- `KEYSTORE_BASE64` — PKCS12 RSA-4096 keystore в base64
- `STORE_PASSWORD` / `KEY_PASSWORD` — одинаковые (PKCS12 требование)
- `KEY_ALIAS` — `freenet`

CI (freenet.yml) декодирует keystore в shell перед Gradle (не в Kotlin DSL).
Secrets уже загружены в `mintfary-oss/zapret2-may` → Settings → Secrets.

SHA-256 fingerprint (для Google Play Console):
`A4:41:EA:E5:63:D0:0D:28:F3:28:FE:6C:3C:F5:7D:37:FE:85:56:C2:A2:13:AF:D9:11:32:66:89:1F:8A:61:30`

### 9.2 Telegram бот ✅

```go
// internal/telegram/bot.go
type Bot struct {
    token  string
    server Controller  // proxy.Server
    chatID int64
}

// Команды:
// /help     — список команд
// /status   — текущее состояние + стратегия + listen addr
// /on       — включить bypass
// /off      — выключить bypass
// /strategy auto|split|tlsrec|combined|disorder|fake|none
// /stats    — активных/всего/обойдено соединений
```

Запуск: `freenet -telegram-token <BOT_TOKEN> [-telegram-chat-id <CHAT_ID>]`

### 9.3 Test coverage 60%+ ✅

- `internal/bypass/relay_test.go` — 10 тестов через `net.Pipe()`
- `internal/proxy/socks5_test.go` — 11 тестов SOCKS5 handshake/request
- `internal/web/ui_test.go` — 12 тестов HTTP handlers
- `internal/telegram/bot_test.go` — 17 тестов команд/dispatch

---

## Phase 10 — Test coverage ~70%+ + GitHub community files ✅ ЗАВЕРШЕНА (v1.9.0)

| Пакет | Файл | Тестов |
|---|---|---|
| `internal/config` | `config_test.go` | 8 |
| `internal/sysproxy` | `sysproxy_test.go` | 3 |
| `internal/windivert` | `windivert_test.go` | 8 |
| `internal/bypass` | `engine_extra_test.go` | 20 |
| `internal/dns` | `client_test.go` | 12 |
| `internal/proxy` | `server_test.go` | 16 |
| `internal/web` | `extra_test.go` | 7 |
| `mobile` | `mobile_test.go` | 28 |

GitHub community files: `.github/CONTRIBUTING.md`, `.github/SECURITY.md`,
`.github/PULL_REQUEST_TEMPLATE.md`, `.github/ISSUE_TEMPLATE/`.

---

## Phase 11 — Test coverage ~90%+ ✅ ЗАВЕРШЕНА (v1.9.1)

| Файл | Пакет | Что тестирует |
|---|---|---|
| `fake_linux_test.go` | bypass | checksums, buildIPv4TCPPacket |
| `quic_test.go` | bypass | IsQUICInitial, RelayQUIC |
| `hostlist_extra_test.go` | bypass | LoadFile, DownloadAndSave |
| `engine_more_test.go` | bypass | SetHTTPClient, RunAutoDetect |
| `resolver_test.go` | dns | NewResolver, Start/Stop, forward |
| `ech_test.go` | dns | parseSVCBECH, LookupECHConfig, EnableECH |
| `nfqueue_test.go` | proxy | parseIPv4TCP, NewNFQueueServer |
| `handleSOCKS_test.go` | proxy | handleSOCKS, RunAutoDetect |
| `lifecycle_test.go` | web | UI.Start/Stop, handleLogsWS |
| `run_test.go` | telegram | Bot.Run, context cancel |

Итоговое покрытие (per-package):

| Пакет | Покрытие |
|---|---|
| `internal/bypass` | **73.2%** |
| `internal/config` | **88.2%** |
| `internal/dns` | **79.5%** |
| `internal/logs` | **100%** |
| `internal/proxy` | **51.2%** |
| `internal/sysproxy` | **100%** |
| `internal/telegram` | **88.8%** |
| `internal/web` | **89.9%** |
| `internal/windivert` | **100%** |
| `mobile` | **33.1%** |

Недостижимо без root/hardware:
- `cmd/freenet` — `main()` с side-effects
- `mobile/tun.go`, `tun_udp.go` — требуют TUN-девайс
- `internal/proxy/transparent.go` — iptables REDIRECT + SO_ORIGINAL_DST
- `internal/proxy/nfqueue.go` — handlePacket (netfilter queue)

---

## Phase 13 — Android Go engine fix + full verification ✅ ЗАВЕРШЕНА (v1.8.1)

### Критический баг (исправлен в v1.8.1)

`FreenetVpnService.kt` — два reflection-бага из-за которых Go engine никогда не стартовал:

```kotlin
// ❌ БЫЛО (неверно):
Class.forName("mobile.Mobile\$SocketProtector")  // класса не существует
.getMethod("startVPN", Long::class.java, ...)     // boxed тип → NoSuchMethodException

// ✅ СТАЛО (верно):
Class.forName("mobile.SocketProtector")           // gomobile: top-level Java класс
.getMethod("startVPN", java.lang.Long.TYPE, java.lang.Integer.TYPE, ...)  // primitive
```

Эффект до фикса: `tryStartGoVPN()` всегда возвращал `false` → fallback на `PacketForwarder.kt`
→ пытался подключиться к `127.0.0.1:1080` где ничего не было → весь трафик падал.

### Верификация на сервере (ARM64 Linux)

```
go test ./...  → все 11 пакетов PASS
go vet ./...   → 0 предупреждений
gofmt -l .     → 0 файлов

Бинарник 7.1 МБ:
  Web UI /          → HTTP 200
  /api/status       → {"enabled":true,"strategy":"auto","dns_enabled":true}
  SOCKS5 handshake  → 05 00 (no-auth ✓)

Кросс-компиляция:
  linux/amd64 ✓  linux/arm64 ✓  linux/mips ✓  linux/mipsle ✓  windows/amd64 ✓

CI run #53: completed / success (freenet-v1.8.1)
Все 13 release assets присутствуют на:
  https://github.com/mintfary-oss/zapret2-may/releases/tag/freenet-v1.8.1
```

---

## Оставшееся (опционально)

| Задача | Важность | Описание |
|--------|----------|---------|
| F-Droid submission | 🟡 | Подать MR в fdroid/fdroiddata на GitLab (требует аккаунт GitLab) |
| macOS .app bundle | 🟢 | Homebrew формула; трей технически работает (getlantern/systray) |
| Windows ARM64 | 🟢 | Нет WinDivert ARM64 — нужно проверить поддержку |
| proxy/transparent тесты | 🟢 | Требуют root + iptables |
| coverage mobile > 60% | 🟢 | Требует TUN-устройство (Android VPN) |

---

## Быстрый старт для разработчика

```bash
# Клонировать
git clone https://github.com/mintfary-oss/zapret2-may.git
cd zapret2-may/go-freenet

# Запустить на Linux/Docker
go run ./cmd/freenet -web :8080
# Или через Docker:
docker compose up -d

# Запустить тесты
go test ./...

# Покрытие по пакетам
for pkg in ./internal/...; do go test -cover "$pkg"; done

# Кросс-компиляция
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64  go build -o dist/freenet-linux-amd64   ./cmd/freenet
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64  go build -o dist/freenet-linux-arm64   ./cmd/freenet
CGO_ENABLED=0 GOOS=linux   GOARCH=mips   GOMIPS=softfloat go build -o dist/freenet-linux-mips ./cmd/freenet
CGO_ENABLED=0 GOOS=windows GOARCH=amd64  go build -o dist/freenet-windows-amd64.exe ./cmd/freenet

# Android AAR (требует Android NDK)
export ANDROID_NDK_HOME=$HOME/Android/Sdk/ndk/26.3.11579264
bash scripts/build-android.sh

# Создать релиз (CI соберёт всё автоматически)
git tag freenet-v1.8.1
git push origin freenet-v1.8.1
```
