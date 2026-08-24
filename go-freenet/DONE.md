# Что сделано — FreeNet Go

## Текущая версия: v1.0.5 — все Phase 1–3 завершены, релиз опубликован

---

## Структура проекта

```
go-freenet/
├── cmd/freenet/
│   ├── main.go                       # точка входа, CLI флаги
│   ├── service_stub.go               # заглушка для не-Windows
│   └── service_windows.go            # Windows Service (Install/Uninstall)
├── internal/
│   ├── bypass/
│   │   ├── engine.go                 # выбор и запуск стратегии обхода DPI
│   │   ├── split.go                  # TCP фрагментация на позиции SNI
│   │   ├── disorder.go               # disorder атака (head/tail + пауза)
│   │   ├── tlsrec.go                 # TLS record layer splitting
│   │   ├── fake.go                   # fake packets — интерфейс
│   │   ├── fake_linux.go             # fake packets — raw socket (Linux)
│   │   ├── fake_stub.go              # fake packets — заглушка (не-Linux)
│   │   ├── quic.go                   # QUIC/HTTP3 bypass (UDP 443)
│   │   ├── tls.go                    # парсер TLS ClientHello / SNI offset
│   │   ├── autodetect.go             # авто-подбор стратегии под провайдера
│   │   └── hostlist.go               # фильтрация по доменам (antizapret)
│   ├── config/
│   │   └── config.go                 # YAML конфигурация
│   ├── logs/
│   │   └── ring.go                   # кольцевой буфер логов + WebSocket pub
│   ├── proxy/
│   │   ├── server.go                 # управление сервером, lifecycle
│   │   ├── socks5.go                 # SOCKS5 прокси (RFC 1928), полный
│   │   ├── transparent.go            # прозрачный прокси, iptables REDIRECT
│   │   ├── transparent_stub.go       # заглушка (Windows/macOS)
│   │   ├── nfqueue.go                # netfilter queue (Linux, ядро)
│   │   ├── nfqueue_stub.go           # заглушка (не-Linux)
│   │   └── stats.go                  # статистика соединений (атомарные)
│   ├── types/
│   │   └── types.go                  # общие структуры (StatsSnapshot, etc.)
│   └── web/
│       └── ui.go                     # веб-UI + WebSocket логи в реальном времени
├── mobile/                           # gomobile-bindable API (публичный пакет)
│   ├── engine.go                     # FreenetEngine — SOCKS5 + VPN API
│   └── tun.go                        # IPv4/TCP TUN packet forwarder
├── android/                          # Android Studio проект
│   ├── app/src/main/
│   │   ├── AndroidManifest.xml       # разрешения VpnService
│   │   └── java/com/freenet/vpn/
│   │       ├── MainActivity.kt       # Compose UI: кнопка, стратегия, лог
│   │       ├── FreenetVpnService.kt  # VpnService + TUN lifecycle
│   │       ├── PacketForwarder.kt    # Kotlin fallback tun2socks
│   │       ├── VpnViewModel.kt       # MVVM state management
│   │       └── BootReceiver.kt       # автозапуск при загрузке
│   ├── gradle/
│   │   ├── libs.versions.toml        # version catalog
│   │   └── wrapper/gradle-wrapper.properties
│   ├── gradle.properties             # android.useAndroidX=true
│   ├── build.gradle.kts
│   └── settings.gradle.kts
├── init.d/systemd/
│   └── freenet.service               # systemd unit
├── scripts/
│   ├── install.sh                    # установщик Linux (systemd)
│   ├── install-windows.ps1           # PowerShell one-liner
│   ├── setup-transparent.sh          # настройка iptables REDIRECT
│   ├── teardown-transparent.sh       # откат iptables
│   ├── build-android.sh              # сборка gomobile AAR
│   └── build-release-apk.sh          # полный pipeline: AAR → APK
├── .github/workflows/
│   └── freenet.yml                   # CI/CD: build + release
├── Dockerfile                        # multi-stage, финальный образ ~15 MB
├── docker-compose.yml                # одна команда запуска
├── docker-entrypoint.sh              # entrypoint с настройкой iptables
├── go.mod / go.sum                   # зависимости
├── CHAT.md                           # переписка с Neo
├── DONE.md                           # этот файл
├── ERRORS.md                         # журнал ошибок
├── PLAN.md                           # план проекта
├── TODO.md                           # задачи
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

---

## Платформы и артефакты

| Платформа | Файл | Способ установки |
|-----------|------|-----------------|
| Android | `freenet-android.apk` | Скачать → установить APK |
| Windows | `freenet-windows-amd64.exe` | `freenet.exe -install` (служба) |
| Windows (авто) | `install-windows.ps1` | PowerShell one-liner |
| Linux x86-64 | `freenet-linux-amd64` | бинарник или installer |
| Linux ARM64 | `freenet-linux-arm64` | бинарник или installer |
| Linux ARMv7 | `freenet-linux-armv7` | Raspberry Pi / роутеры |
| Linux bundle | `freenet-linux-amd64-installer.tar.gz` | tar + `sudo bash install.sh` |
| Linux (авто) | `install.sh` | `curl … \| sudo bash` |
| Docker | — | `docker compose up -d` |
| Android AAR | `mobile.aar` | для разработчиков |

---

## GitHub Actions CI/CD

Триггеры:
- **Push в master/main/neo/\*\*** — сборка без релиза
- **Tag `freenet-v*.*.*`** — сборка + создание GitHub Release с артефактами
- **Pull Request** — сборка для проверки

Jobs:
- `Lint` — `go vet` + `gofmt`
- `Build` (matrix: linux/amd64, arm64, armv7, windows/amd64) — кросс-компиляция
- `Android APK` — gomobile AAR + Gradle APK
- `Package Linux installer` — installer.tar.gz
- `Create GitHub Release` — только при tag-пуше

---

## Как запустить

```bash
# Docker (рекомендуется)
cd go-freenet && docker compose up -d
# → http://localhost:8080  (веб UI с большой кнопкой)
# → 127.0.0.1:1080        (SOCKS5 прокси)

# Linux напрямую
go build -o freenet ./cmd/freenet && ./freenet -web :8080

# Linux как сервис
sudo bash scripts/install.sh

# Windows как сервис
freenet-windows-amd64.exe -install

# Android APK (при наличии NDK)
bash scripts/build-release-apk.sh
```

---

## Зависимости

| Пакет | Версия | Назначение |
|-------|--------|-----------|
| `github.com/gorilla/websocket` | v1.5.3 | WebSocket для логов |
| `github.com/florianl/go-nfqueue/v2` | v2.1.0 | Netfilter queue (Linux) |
| `golang.org/x/sys` | v0.47.0 | Системные вызовы (unix/windows) |
| `golang.org/x/mobile` | v0.0.0-20260821 | gomobile для Android |
| `gopkg.in/yaml.v3` | v3.0.1 | YAML конфигурация |
