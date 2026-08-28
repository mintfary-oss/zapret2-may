# Что сделано — FreeNet Go

## Текущая версия: v1.9.6 — Phase 20: Hotfix — VPN зависал в CONNECTING (ACCESS_NETWORK_STATE) ✅

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
│   ├── diagnostics/
│   │   ├── monitor.go                # Monitor: подписка на ring, счётчики ошибок, BuildReport()
│   │   └── monitor_test.go           # 3 теста: ErrorCount, BuildReport, formatBytes
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

## Phase 20 — Hotfix: VPN зависал в CONNECTING (v1.9.6) ✅

### Проблема
После обновления до v1.9.5 VPN зависал в состоянии «ПОДКЛЮЧЕНИЕ...» навсегда и никогда не подключался.

### Причина
`setUnderlyingNetworksCompat()` вызывает `ConnectivityManager.getNetworkCapabilities()`, которому нужно разрешение `ACCESS_NETWORK_STATE`. Без него — `SecurityException`. Исключение ловилось в `startVpn()`, но `ACTION_STOP` не отправлялся (потому что `stopVpn()` выходит если `isRunning == false`). UI навсегда застревал в CONNECTING.

### Исправления ✅

| Исправление | Файл |
|-------------|------|
| `ACCESS_NETWORK_STATE` добавлен в манифест | AndroidManifest.xml |
| `setUnderlyingNetworksCompat()` обёрнут в try-catch | FreenetVpnService.kt |
| `ACTION_STOP` явно отправляется при ошибке `startVpn()` | FreenetVpnService.kt |

---

## Phase 19 — ERR_NETWORK_CHANGED fix + Auto-detect on connect + Reload banner (v1.9.5) ✅

### Проблема
После включения VPN браузер выдавал ERR_NETWORK_CHANGED и переставал загружать сайты. Стратегия auto на Android никогда не проводила реального пробинга — движок работал на дефолтном tlsrec без подстройки под провайдера. Пользователю было непонятно, что нужно обновить страницу.

### 19.1 setUnderlyingNetworks — fixes ERR_NETWORK_CHANGED ✅

`FreenetVpnService.setUnderlyingNetworksCompat()` вызывается сразу после `VpnService.Builder.establish()`:

- Получает список активных физических сетей через `ConnectivityManager.allNetworks`
- Фильтрует: только сети с `NET_CAPABILITY_INTERNET`, без `TRANSPORT_VPN`
- Передаёт их в `VpnService.setUnderlyingNetworks(networks)`
- Fallback: если сети не найдены → `setUnderlyingNetworks(null)` (Android выбирает сам)

**Эффект:** Android помечает VPN как «работающий поверх текущей сети». Chrome/Edge не видят TUN как отдельную сеть → ERR_NETWORK_CHANGED исчезает.

### 19.2 RunAutoDetect на Android ✅

| Компонент | Изменение |
|-----------|-----------|
| `bypass/autodetect.go` | Цель пробинга: `www.youtube.com:443` (заблокирован у РФ провайдеров) |
| `bypass/autodetect.go` | Дефолт до завершения пробинга: `split` (вместо `tlsrec`) |
| `bypass/autodetect.go` | `GlobalDetector()` — публичный аксессор к синглтону |
| `mobile/engine.go` | `RunAutoDetect()` — запускает горутину пробинга, применяет winner |
| `FreenetVpnService.kt` | Вызов `runAutoDetect()` через reflection сразу после старта VPN |

**Эффект:** При каждом включении VPN движок автоматически подбирает лучшую стратегию для текущего провайдера за ~5–15 секунд в фоне.

### 19.3 ReloadBrowserBanner ✅

Зелёная карточка появляется при каждом подключении VPN:
- **«Открыть браузер»** — запускает дефолтный браузер через `Intent.CATEGORY_APP_BROWSER`, скрывает карточку
- **«Понятно»** — скрывает карточку (без persist — появится при следующем подключении)
- State хранится в `VpnViewModel._reloadBannerVisible` (не SharedPreferences)

---

## Phase 18 — Auto-block DoT port 853 + DNS Setup Card (v1.9.1) ✅

### Проблема
Браузер не загружал сайты при включённом VPN. Android **Private DNS** и Chrome **Secure DNS**
отправляли DNS-запросы по протоколу DNS-over-TLS (DoT, порт 853). Этот порт:
1. Заблокирован российскими провайдерами (ТСПУ)
2. Обходил DoH-резолвер FreeNet (TUN перехватывал только UDP порт 53)

### 18.1 Go TUN (`mobile/tun.go`) — автоматически ✅

| Пакет | Действие | Эффект |
|-------|----------|--------|
| UDP порт 853 | Drop | DoT над UDP блокируется |
| TCP SYN порт 853 | RST | DoT над TCP блокируется, немедленный fallback |

Chrome/Android получают мгновенный отказ и переключаются на UDP DNS 53 → FreeNet
перехватывает и резолвит через DoH. Никаких действий пользователя не требуется.

### 18.2 Android UI (`MainActivity.kt`, `VpnViewModel.kt`) ✅

- **`DnsSetupCard`** — карточка с тёплым фоном, появляется при первом подключении VPN
- **Android 10+**: кнопка «Открыть настройки DNS» → `Settings.ACTION_PRIVATE_DNS_SETTINGS` (один тап)
- **«Понятно»** — скрывает карточку навсегда (SharedPreferences `dns_banner_dismissed`)
- Объяснение Chrome Secure DNS (`chrome://settings/security`)

### 18.3 Что осталось ручным

| Шаг | Почему нельзя автоматизировать |
|-----|-------------------------------|
| Отключение Private DNS в настройках Android | Требует системные привилегии |
| Отключение Secure DNS в Chrome | Chrome не предоставляет API для сторонних приложений |

---

## Phase 17 — Diagnostics Monitor + Report Tab (v1.9.0) ✅

### 17.1 `internal/diagnostics/monitor.go`

- `Monitor` подписывается на `logs.Ring` (ring buffer логов) и отслеживает ошибки/предупреждения в реальном времени через `watch()` goroutine
- Счётчики: `ErrorCount()` / `WarnCount()` (атомарные)
- `BuildReport(version, srv StatusProvider) string` — генерирует полный текстовый отчёт:
  - Версия, время старта, uptime
  - Статус DPI bypass + текущая стратегия + список доменов
  - Статистика соединений (активных/всего/обойдено/байты)
  - DNS-over-HTTPS и ECH счётчики
  - Список ошибок и предупреждений из лога
  - Последние 200 строк журнала

### 17.2 Web UI — вкладка "📊 Диагностика"

- Новый HTTP endpoint `/api/report` → `text/plain`
- Новая вкладка **📊 Диагностика** в `internal/web/ui.go`:
  - **🔄 Обновить** — перезагрузить отчёт
  - **📋 Скопировать** — в буфер обмена (Clipboard API)
  - **⬇️ Скачать .txt** — `freenet-report-<timestamp>.txt` через Blob URL
- `UI.SetReporter(fn func() string)` — слабое связывание, нет import cycle

### 17.3 Тесты

| Файл | Что тестирует | Результат |
|---|---|---|
| `monitor_test.go` | `ErrorCount`, `WarnCount`, `BuildReport`, `formatBytes` | ✅ 3/3 PASS |

### 17.4 Подключение в `main.go`

```go
mon := diagnostics.NewMonitor(ring)
ui.SetReporter(func() string { return mon.BuildReport(version, srv) })
```

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

---

## Phase 10 — Test coverage ~70%+ + GitHub community files (v1.9.0) ✅

### 10.1 Test coverage ~70%+ ✅

Добавлены тесты во все пакеты, где возможно без root/TUN/WinAPI:

| Пакет | До | После | Файл |
|---|---|---|---|
| `internal/config` | 0% | ~88% | `config_test.go` |
| `internal/sysproxy` | 0% | 100% | `sysproxy_test.go` |
| `internal/windivert` | 0% | 100% | `windivert_test.go` |
| `internal/bypass` | 30% | ~50% | `engine_extra_test.go` |
| `internal/dns` | 19% | ~42% | `client_test.go` |
| `internal/proxy` | 15% | ~38% | `server_test.go` |
| `internal/web` | 46% | ~54% | `extra_test.go` |
| `mobile` | 0% | ~33% | `mobile_test.go` |
| `internal/telegram` | 0% | ~65% | `bot_test.go` |

Суммарное покрытие: **21.8% → ~44%**

### 10.2 GitHub community files ✅

- `.github/CONTRIBUTING.md` — инструкция для контрибьюторов
- `.github/SECURITY.md` — политика безопасности + bug bounty
- `.github/PULL_REQUEST_TEMPLATE.md` — шаблон PR
- `.github/ISSUE_TEMPLATE/bug_report.md` — шаблон баг-репорта
- `.github/ISSUE_TEMPLATE/feature_request.md` — шаблон feature request

---

## Phase 14 — Критический фикс Android: неверный Java-пакет gomobile (v1.8.2) ✅

**Корень проблемы — почему Android APK никогда не работал:**

CI использует `gomobile bind -javapkg com.freenet.bypass`, поэтому все Java-классы
находятся в пакете `com.freenet.bypass`, а не `mobile`:

| Что искал код | Что реально в AAR |
|---|---|
| `mobile.FreenetEngine` | `com.freenet.bypass.FreenetEngine` |
| `mobile.Mobile` | `com.freenet.bypass.Mobile` |
| `mobile.SocketProtector` | `com.freenet.bypass.SocketProtector` |

`Class.forName("mobile.FreenetEngine")` → `ClassNotFoundException` (silent catch)
→ `goEngine = null` → `tryStartGoVPN()` возвращал `false` сразу
→ fallback на PacketForwarder → трафик в никуда.

**Исправление в `FreenetVpnService.kt`:**
```kotlin
// initGoEngine():
Class.forName("com.freenet.bypass.Mobile").getMethod("newFreenetEngine")

// tryStartGoVPN():
Class.forName("com.freenet.bypass.SocketProtector")
```

**Версии:** `versionCode = 182`, `versionName = "1.8.2"`, тег `freenet-v1.8.2`.

APK после CI: https://github.com/mintfary-oss/zapret2-may/releases/download/freenet-v1.8.2/freenet-android.apk

---

## Phase 13 — Android Go engine reflection fix (v1.8.1) ✅

**Критический баг исправлен:** Go engine в `FreenetVpnService.kt` никогда не запускался
из-за двух reflection-ошибок (silent catch → fallback → трафик падал).

| Баг | Было | Стало |
|-----|------|-------|
| Имя класса | `"mobile.Mobile$SocketProtector"` | `"mobile.SocketProtector"` |
| Тип параметра | `Long::class.java` (boxed) | `java.lang.Long.TYPE` (primitive) |

**Верификация на ARM64 сервере:**
- `go test ./...` → 11 пакетов PASS
- `go vet ./...` → 0 предупреждений
- Web UI HTTP 200, `/api/status` JSON, SOCKS5 `05 00` ✓
- Кросс-компиляция linux/amd64, arm64, mips, mipsle, windows/amd64 ✓
- CI run #53 `completed / success`, релиз freenet-v1.8.1: 13 artifacts

APK: https://github.com/mintfary-oss/zapret2-may/releases/download/freenet-v1.8.1/freenet-android.apk

---

## Phase 12 — Smoke testing + Platform verification (v1.9.1) ✅

Полное дымовое тестирование на ARM64 Linux сервере (August 2026):

| Тест | Результат |
|------|-----------|
| `go build` Linux (7.1 МБ) | ✅ |
| Запуск + `HTTP 200` веб-UI | ✅ |
| `/api/status` JSON | ✅ |
| `/api/stats` JSON | ✅ |
| SOCKS5 handshake `0x0500` | ✅ |
| Cross-compile windows/amd64 | ✅ 8.0 МБ |
| Cross-compile linux/amd64 | ✅ 7.6 МБ |
| Cross-compile linux/arm64 | ✅ 7.0 МБ |
| Cross-compile linux/mips | ✅ 8.2 МБ |
| Cross-compile linux/mipsle | ✅ 8.2 МБ |
| `go test ./...` | ✅ PASS |
| `go vet ./...` | ✅ CLEAN |

---

## Phase 11 — Test coverage ~90%+ (v1.9.1) ✅

Цель: довести покрытие с 43.8% до максимально возможного без root/TUN-устройств.

### 11.1 Новые тест-файлы ✅

| Файл | Что покрывает | Пакет |
|---|---|---|
| `fake_linux_test.go` | `buildIPv4TCPPacket`, `ipv4Checksum`, `tcpv4Checksum`, `onesComplementSum` | bypass |
| `quic_test.go` | `IsQUICInitial`, `RelayQUIC` (split/plain/invalid) | bypass |
| `hostlist_extra_test.go` | `LoadFile`, `DownloadAndSave`, `loadFile`, `SetHTTPClient` | bypass |
| `engine_more_test.go` | `SetHTTPClient`, `RunAutoDetect`, NewEngine с файлом/auto-update | bypass |
| `resolver_test.go` | `NewResolver`, `Start`/`Stop`, контекст, `forward`, UDP запросы | dns |
| `ech_test.go` | `parseSVCBECH`, `LookupECHConfig`, `EnableECH` | dns |
| `nfqueue_test.go` | `parseIPv4TCP`, `NewNFQueueServer`, `SetEnabled` | proxy |
| `handleSOCKS_test.go` | `handleSOCKS` (refused/passthrough/bypass), `RunAutoDetect` | proxy |
| `lifecycle_test.go` | `UI.Start`/`Stop`, `handleLogsWS` (connect/live/disconnect) | web |
| `run_test.go` | `Bot.Run` (context cancel, update dispatch, retry on error) | telegram |

### 11.2 Итоговое покрытие ✅

| Пакет | До | После |
|---|---|---|
| `internal/bypass` | 49.9% | **73.2%** |
| `internal/config` | — | **88.2%** |
| `internal/dns` | 42.0% | **79.5%** |
| `internal/logs` | — | **100%** |
| `internal/proxy` | 37.5% | **51.2%** |
| `internal/sysproxy` | — | **100%** |
| `internal/telegram` | 65.2% | **86.5%** |
| `internal/web` | 53.6% | **89.9%** |
| `internal/windivert` | — | **100%** |
| `mobile` | 33.1% | **33.1%** (TUN — без root) |
| **Суммарно** | **43.8%** | **59.8%** |

Недостижимо без root/hardware (исключено):
- `cmd/freenet` — `main()` с side-effects
- `mobile/tun.go`, `mobile/tun_udp.go` — требуют TUN-девайс
- `internal/proxy/transparent.go` — iptables REDIRECT + `SO_ORIGINAL_DST`
- `internal/proxy/nfqueue.go` — `handlePacket`/`reinjectSplit` (требуют netfilter queue)
