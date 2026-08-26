# Что сделано — FreeNet Go

## Текущая версия: v1.8.0 — Phase 1–9 завершены

---

## Структура проекта

```
go-freenet/
├── cmd/freenet/
│   ├── main.go                       # точка входа, CLI флаги (v1.6.0)
│   ├── service_stub.go               # заглушка для не-Windows
│   ├── service_windows.go            # Windows Service (Install/Uninstall)
│   ├── tray_windows.go               # системный трей + WinDivert статус
│   ├── tray_stub.go                  # заглушка трея (Linux/macOS)
│   ├── windivert_windows.go          # lifecycle WinDivert (start/stop/restart)
│   └── windivert_stub.go             # заглушка WinDivert (Linux/macOS)
├── internal/
│   ├── bypass/
│   │   ├── engine.go                 # выбор и запуск стратегии + ECH passthrough
│   │   ├── split.go                  # TCP фрагментация на позиции SNI
│   │   ├── disorder.go               # disorder атака (head/tail + пауза)
│   │   ├── tlsrec.go                 # TLS record layer splitting
│   │   ├── fake.go                   # fake packets — интерфейс
│   │   ├── fake_linux.go             # fake packets — raw socket (Linux)
│   │   ├── fake_stub.go              # fake packets — заглушка (не-Linux)
│   │   ├── quic.go                   # QUIC/HTTP3 bypass (UDP 443)
│   │   ├── tls.go                    # парсер TLS ClientHello / SNI / ECH (0xFE0D)
│   │   ├── autodetect.go             # авто-подбор стратегии под провайдера
│   │   └── hostlist.go               # фильтрация по доменам (antizapret)
│   ├── config/
│   │   └── config.go                 # YAML конфигурация (incl. DNSConfig)
│   ├── dns/
│   │   ├── doh.go                    # DoH-клиент RFC 8484 + ECH lookup (RFC 9460)
│   │   └── resolver.go               # локальный UDP→DoH резолвер (127.0.0.1:5300)
│   ├── logs/
│   │   └── ring.go                   # кольцевой буфер логов + WebSocket pub
│   ├── proxy/
│   │   ├── server.go                 # управление сервером, lifecycle, DoH + ECH
│   │   ├── socks5.go                 # SOCKS5 прокси (RFC 1928), полный
│   │   ├── transparent.go            # прозрачный прокси, iptables REDIRECT
│   │   ├── transparent_stub.go       # заглушка (Windows/macOS)
│   │   ├── nfqueue.go                # netfilter queue (Linux, ядро)
│   │   ├── nfqueue_stub.go           # заглушка (не-Linux)
│   │   └── stats.go                  # статистика соединений (атомарные)
│   ├── sysproxy/
│   │   ├── sysproxy_windows.go       # реестр: set/restore системного прокси
│   │   ├── sysproxy_stub.go          # заглушка (не-Windows)
│   │   └── notify_windows.go         # WM_SETTINGCHANGE broadcast
│   ├── types/
│   │   └── types.go                  # общие структуры (StatsSnapshot, etc.)
│   ├── windivert/
│   │   ├── windivert.go              # пакет-заголовок + doc
│   │   ├── windivert_windows.go      # DLL loader (syscall.NewLazyDLL) + перехват + TLS split
│   │   └── windivert_stub.go         # заглушка (Linux/macOS)
│   └── web/
│       └── ui.go                     # веб-UI + WebSocket логи + DoH + ECH статус
├── mobile/                           # gomobile-bindable API (публичный пакет)
│   ├── engine.go                     # FreenetEngine — SOCKS5 + VPN API
│   ├── tun.go                        # IPv4 TUN: TCP + DNS→DoH + UDP dispatch
│   └── tun_udp.go                    # UDP NAT relay (Discord, Steam, игры)
├── android/                          # Android Studio проект
│   ├── app/src/main/
│   │   ├── AndroidManifest.xml       # VpnService + FreeNetWidget
│   │   ├── res/
│   │   │   ├── values/strings.xml    # строки приложения + виджет + split tunnel
│   │   │   ├── layout/widget_toggle.xml  # layout 2×1 кнопки виджета
│   │   │   └── xml/freenet_widget_info.xml  # AppWidget метаданные
│   │   └── java/com/freenet/vpn/
│   │       ├── MainActivity.kt       # Compose UI + SplitTunnelCard
│   │       ├── FreenetVpnService.kt  # VpnService + split tunnel builder
│   │       ├── FreeNetWidget.kt      # AppWidgetProvider (2×1 toggle)
│   │       ├── SplitTunnelConfig.kt  # per-app SharedPreferences конфиг
│   │       ├── PacketForwarder.kt    # Kotlin fallback tun2socks
│   │       ├── VpnViewModel.kt       # MVVM + splitTunnel StateFlow
│   │       └── BootReceiver.kt       # автозапуск при загрузке
│   ├── app/src/androidTest/java/com/freenet/vpn/
│   │   ├── SplitTunnelConfigTest.kt  # 10 instrumented тестов SharedPreferences (v1.7.0)
│   │   ├── VpnServiceTest.kt         # 7 instrumented тестов класса/интентов (v1.7.0)
│   │   └── PacketForwarderTest.kt    # 10 instrumented тестов checksum+структуры (v1.7.0)
│   ├── gradle/
│   │   ├── libs.versions.toml        # version catalog
│   │   └── wrapper/gradle-wrapper.properties
│   ├── gradle.properties             # android.useAndroidX=true
│   ├── build.gradle.kts
│   └── settings.gradle.kts
├── init.d/systemd/
│   └── freenet.service               # systemd unit
├── init.d/openwrt/
│   └── freenet                       # procd init.d скрипт для OpenWrt (v1.7.0)
├── scripts/
│   ├── install.sh                    # установщик Linux (systemd)
│   ├── install-windows.ps1           # PowerShell one-liner (скачивает bundle с WinDivert)
│   ├── setup-transparent.sh          # настройка iptables REDIRECT
│   ├── teardown-transparent.sh       # откат iptables
│   ├── build-android.sh              # сборка gomobile AAR
│   └── build-release-apk.sh          # полный pipeline: AAR → APK
├── .github/workflows/
│   └── freenet.yml                   # CI/CD: lint + build (incl. MIPS) + android + release
├── Dockerfile                        # multi-stage, финальный образ ~15 MB
├── docker-compose.yml                # одна команда запуска
├── docker-entrypoint.sh              # entrypoint с настройкой iptables
├── go.mod / go.sum                   # зависимости
├── CHAT.md                           # переписка с Neo
├── DONE.md                           # этот файл
├── ERRORS.md                         # журнал ошибок и исправлений
├── PLAN.md                           # план проекта
├── TODO.md                           # задачи и будущие фазы
└── TECHNICAL.md                      # техническая документация
```

---

## Реализованные стратегии обхода DPI

| Стратегия | Описание | Привилегии | Эффективность |
|-----------|----------|------------|---------------|
| `split` | TCP фрагментация ClientHello на позиции SNI | нет | Средняя |
| `disorder` | Перестановка сегментов head/tail с паузой | нет | Средняя |
| `tlsrec` | TLS record layer splitting (2 TLS записи) | нет | Высокая |
| `fake` | Decoy пакет (TTL=4 или bad checksum) + split | `CAP_NET_RAW` | Высокая |
| `combined` | fake + tlsrec одновременно | `CAP_NET_RAW` | Максимальная |
| `quic` | QUIC Initial datagram фрагментация (UDP 443) | нет | Средняя |
| `auto` | Авто-тест всех стратегий, выбор лучшей | — | Адаптивная |
| `none` | Без обхода (для отладки) | — | — |
| **WinDivert** | Перехват пакетов на уровне ядра (Windows) | Администратор | Максимальная |

---

## DNS защита

| Компонент | Что делает |
|-----------|-----------|
| `internal/dns/doh.go` | DoH-клиент RFC 8484: POST к 1.1.1.1/8.8.8.8/9.9.9.9 |
| `internal/dns/resolver.go` | UDP резолвер 127.0.0.1:5300 → DoH |
| `mobile/tun.go` | Android TUN: UDP:53 → DoH (без root) |
| `mobile/tun_udp.go` | Android TUN: прочий UDP → UDP NAT relay |
| `internal/bypass/hostlist.go` | Загрузка списков через DoH-aware HTTP |
| `internal/proxy/server.go` | Запуск DoH + ECH resolver при старте |
| `internal/web/ui.go` | DoH + ECH статус в веб UI |

---

## ECH (Encrypted Client Hello)

| Компонент | Что делает |
|-----------|-----------|
| `internal/bypass/tls.go` | Парсит extension `0xFE0D` → `HasECH bool` |
| `internal/bypass/engine.go` | ECH → passthrough без bypass (нет смысла разрывать) |
| `internal/dns/doh.go` | `LookupECHConfig()` из DNS HTTPS (RFC 9460), `EnableECH()` |
| `internal/web/ui.go` | `🔐 ECH обнаружен: N соед.` в веб UI |

---

## Android (v1.7.0)

| Компонент | Что делает |
|-----------|-----------|
| `mobile/tun.go` | IPv4 TCP proxy + DNS→DoH + UDP relay dispatch |
| `mobile/tun_udp.go` | UDP NAT: Discord/Steam/игры через VPN |
| `FreenetVpnService.kt` | VpnService + IPv4-only routing + split tunnel |
| `SplitTunnelConfig.kt` | Per-app: disabled/allowlist/blocklist (SharedPreferences) |
| `FreeNetWidget.kt` | AppWidget 2×1: зелёная/красная кнопка на рабочем столе |
| `VpnViewModel.kt` | MVVM + `splitTunnel: StateFlow<SplitTunnelConfig>` |
| `MainActivity.kt` | Compose UI + `SplitTunnelCard` (поиск + чекбоксы) |

---

## Платформы и артефакты (GitHub Releases)

| Платформа | Файл | Способ установки |
|-----------|------|-----------------|
| 🤖 Android | `freenet-android.apk` | Скачать → установить APK |
| 🪟 Windows (рекомендуется) | `freenet-windows-bundle.zip` | Распаковать → `freenet.exe -install` (Admin) |
| 🪟 Windows (bare exe) | `freenet-windows-amd64.exe` | `freenet.exe -install` (без WinDivert) |
| 🪟 Windows (авто) | `install-windows.ps1` | PowerShell one-liner |
| 🐧 Linux x86-64 | `freenet-linux-amd64` | бинарник или installer |
| 🐧 Linux ARM64 | `freenet-linux-arm64` | бинарник или installer |
| 🐧 Linux ARMv7 | `freenet-linux-armv7` | Raspberry Pi / роутеры |
| 📡 OpenWrt MIPS | `freenet-linux-mips` | TP-Link WR841, Netgear WNR |
| 📡 OpenWrt MIPSle | `freenet-linux-mipsle` | MediaTek MT7620/MT7621 |
| 🐧 Linux bundle | `freenet-linux-amd64-installer.tar.gz` | tar + `sudo bash install.sh` |
| 🐧 Linux (авто) | `install.sh` | `curl … | sudo bash` |
| 🐳 Docker | — | `docker compose up -d` |
| 📦 Android AAR | `mobile.aar` | Для разработчиков |

---

## GitHub Actions CI/CD

Триггеры:
- **Push в master/main/neo/\*\*** — сборка + тесты без релиза
- **Tag `freenet-v*.*.*`** — сборка + GitHub Release со всеми артефактами
- **Pull Request** — сборка для проверки

Jobs:
- `Lint` — `go vet` + `gofmt`
- `Build` (matrix: linux/amd64, arm64, armv7, windows/amd64) — кросс-компиляция
- `Android APK` — gomobile AAR + Gradle APK
- `Package Linux installer` — installer.tar.gz
- `Package Windows bundle` — WinDivert (latest) + freenet.exe → ZIP
- `Create GitHub Release` — только при tag-пуше

---

## Как запустить

```bash
# Docker (рекомендуется для Linux/сервера)
cd go-freenet && docker compose up -d
# → http://localhost:8080  (веб UI с большой кнопкой)
# → 127.0.0.1:1080        (SOCKS5 прокси)
# → 127.0.0.1:5300        (локальный DoH резолвер, UDP)

# Linux напрямую
go build -o freenet ./cmd/freenet && ./freenet -web :8080

# Linux как сервис
sudo bash scripts/install.sh

# Windows (рекомендуется: bundle с WinDivert)
# Скачать freenet-windows-bundle.zip → распаковать → запустить от Admin:
freenet.exe -install

# Android APK
# Скачать freenet-android.apk с GitHub Releases → установить
# Или собрать: bash scripts/build-release-apk.sh
```


## Phase 9 — Telegram бот + Release signing + тесты (v1.8.0) ✅

### 9.1 Android release signing
`build.gradle.kts` — `signingConfigs` на env-переменных (`KEYSTORE_BASE64`,
`STORE_PASSWORD`, `KEY_ALIAS`, `KEY_PASSWORD`). CI собирает signed release APK
автоматически, unsigned для F-Droid.

### 9.2 Test coverage ~60%+
- `internal/bypass/relay_test.go` — 10 тестов (split/tlsrec/disorder через net.Pipe)
- `internal/proxy/socks5_test.go` — 11 тестов (handshake/request/stats)
- `internal/web/ui_test.go` — 12 тестов (status/toggle/strategy/autodetect/index)
- `internal/telegram/bot_test.go` — 17 тестов (commands/dispatch/HTTP)

### 9.3 Telegram бот
`internal/telegram/bot.go` — long-polling, без внешних зависимостей.
Команды: `/status /on /off /strategy <name> /stats`.
Интеграция: `TelegramConfig` в `config.go`, флаги `-telegram-token`,
`-telegram-chat-id` в `main.go`.

---

---

## Зависимости

| Пакет | Версия | Назначение |
|-------|--------|-----------|
| `github.com/gorilla/websocket` | v1.5.3 | WebSocket для real-time логов |
| `github.com/florianl/go-nfqueue/v2` | v2.1.0 | Netfilter queue (Linux, ядро) |
| `golang.org/x/sys` | v0.47.0 | Системные вызовы (unix/windows) |
| `golang.org/x/net` | v0.58.0 | `dns/dnsmessage` DoH wire format |
| `golang.org/x/mobile` | v0.0.0-20260821 | gomobile для Android |
| `gopkg.in/yaml.v3` | v3.0.1 | YAML конфигурация |
| `github.com/getlantern/systray` | v1.2.2 | Системный трей Windows/Linux |
