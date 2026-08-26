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
| 11 | Test coverage ~90%+ (Phase 11) | v1.9.1 | ✅ Завершена |
| 12 | Smoke testing + Platform verification | v1.9.1 | ✅ Завершена |

---

## Phase 8 — F-Droid + OpenWrt + качество ✅ ЗАВЕРШЕНА (v1.7.0)

### 8.1 F-Droid манифест ✅

`metadata/com.freenet.vpn.yml` в корне репо. Сборка без gomobile AAR —
`FreenetVpnService` автоматически переключается на `PacketForwarder.kt`.

### 8.2 Android integration tests ✅

- `androidTest/SplitTunnelConfigTest.kt` — 10 тестов SharedPreferences
- `androidTest/VpnServiceTest.kt` — 7 тестов класса/интентов
- `androidTest/PacketForwarderTest.kt` — 10 тестов IP/TCP checksum

### 8.3 OpenWrt procd init.d скрипт ✅

`go-freenet/init.d/openwrt/freenet` — procd-скрипт с respawn,
auto-config, reload через SIGHUP.

### 8.4 Go bypass benchmarks ✅

`internal/bypass/relay_bench_test.go` — бенчмарки throughput через
`net.Pipe()`: plain, split, disorder, tlsrec + парсинг ClientHello.

### 8.5 CI: OpenWrt MIPS сборки ✅

Добавлены в матрицу: `linux/mips` (softfloat) и `linux/mipsle` (softfloat).

---

## Phase 9 — Telegram бот

```go
// internal/telegram/bot.go
type Bot struct {
    token  string
    server *proxy.Server
    chatID int64
}

func (b *Bot) Start(ctx context.Context) {
    // /status   — текущее состояние
    // /on       — включить bypass
    // /off      — выключить bypass
    // /strategy auto|split|tlsrec|combined
    // /stats    — статистика соединений
}
```

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

# Кросс-компиляция
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -o dist/freenet-linux-amd64   ./cmd/freenet
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -o dist/freenet-linux-arm64   ./cmd/freenet
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/freenet-windows-amd64.exe ./cmd/freenet

# Android AAR (требует Android NDK)
export ANDROID_NDK_HOME=$HOME/Android/Sdk/ndk/26.3.11579264
bash scripts/build-android.sh

# Создать релиз (CI соберёт всё автоматически)
git tag freenet-v1.7.0
git push origin freenet-v1.7.0
```
