# Что сделано — FreeNet Go

## Статус: v0.3.0 — Phase 3 (Android) завершена

---

## Структура проекта

```
go-freenet/
├── cmd/freenet/main.go                   # точка входа, CLI флаги
├── internal/
│   ├── bypass/
│   │   ├── engine.go                     # выбор и запуск стратегии
│   │   ├── split.go                      # TCP фрагментация (split)
│   │   ├── disorder.go                   # disorder атака
│   │   ├── tlsrec.go                     # TLS record layer splitting
│   │   ├── fake.go                       # fake packets — интерфейс
│   │   ├── fake_linux.go                 # fake packets — raw socket (Linux)
│   │   ├── fake_stub.go                  # fake packets — заглушка (не-Linux)
│   │   ├── quic.go                       # QUIC/HTTP3 bypass
│   │   ├── tls.go                        # парсер TLS ClientHello / SNI
│   │   ├── autodetect.go                 # авто-подбор стратегии
│   │   └── hostlist.go                   # фильтрация по доменам
│   ├── config/config.go                  # YAML конфигурация
│   ├── logs/ring.go                      # кольцевой буфер логов
│   ├── mobile/                           # ✨ NEW — gomobile Android binding
│   │   ├── engine.go                     # FreenetEngine API (SOCKS5 + VPN)
│   │   └── tun.go                        # IPv4/TCP TUN packet forwarder
│   ├── proxy/
│   │   ├── server.go                     # управление сервером
│   │   ├── socks5.go                     # SOCKS5 прокси (RFC 1928)
│   │   ├── transparent.go                # прозрачный прокси (Linux)
│   │   ├── transparent_stub.go           # заглушка (не-Linux)
│   │   ├── nfqueue.go                    # netfilter queue (Linux)
│   │   ├── nfqueue_stub.go               # заглушка (не-Linux)
│   │   └── stats.go                      # статистика соединений
│   ├── types/types.go                    # общие типы данных
│   └── web/ui.go                         # веб-интерфейс + WebSocket
├── android/                              # ✨ NEW — Android Studio проект
│   ├── app/
│   │   ├── src/main/
│   │   │   ├── AndroidManifest.xml       # VpnService permissions
│   │   │   ├── java/com/freenet/vpn/
│   │   │   │   ├── MainActivity.kt       # Compose UI (big button)
│   │   │   │   ├── FreenetVpnService.kt  # VpnService + TUN lifecycle
│   │   │   │   ├── PacketForwarder.kt    # Kotlin fallback tun2socks
│   │   │   │   ├── VpnViewModel.kt       # state management (MVVM)
│   │   │   │   └── BootReceiver.kt       # auto-start on boot
│   │   │   └── res/                      # strings, drawables, launcher
│   │   ├── build.gradle.kts              # Gradle app config
│   │   └── proguard-rules.pro
│   ├── gradle/
│   │   ├── libs.versions.toml            # version catalog
│   │   └── wrapper/gradle-wrapper.properties
│   ├── build.gradle.kts
│   └── settings.gradle.kts
├── init.d/systemd/freenet.service        # systemd unit
├── scripts/
│   ├── install.sh                        # установщик Linux
│   ├── setup-transparent.sh              # настройка iptables
│   ├── teardown-transparent.sh           # откат iptables
│   ├── build-android.sh                  # ✨ NEW — собирает gomobile AAR
│   └── build-release-apk.sh             # ✨ NEW — полный pipeline APK
├── .github/workflows/build.yml           # CI/CD (+ Android AAR job) ✨ NEW
├── Dockerfile                            # multi-stage build
├── docker-compose.yml                    # одна команда запуска
├── docker-entrypoint.sh                  # entrypoint с iptables
└── go.mod / go.sum                       # зависимости
```

---

## Android APK — Phase 3

### Архитектура

```
Android APK
├── FreenetVpnService (Kotlin)
│   ├── VpnService.Builder → TUN interface (10.89.0.2/24)
│   ├── Go FreenetEngine (gomobile AAR)
│   │   ├── SOCKS5 прокси → 127.0.0.1:1080
│   │   └── ForwardTUN (IPv4/TCP forwarder)
│   └── Fallback: PacketForwarder.kt (без AAR)
├── MainActivity (Compose)
│   ├── Большая кнопка ВКЛЮЧИТЬ/ВЫКЛЮЧИТЬ
│   ├── Выбор стратегии (auto/split/tlsrec/combined/fake/none)
│   ├── Статистика соединений
│   └── Лог в реальном времени
└── BootReceiver — автозапуск при загрузке
```

### Как собрать

```bash
# 1. Требования: Go 1.21+, Android NDK 26+, Android Studio
export ANDROID_NDK_HOME=$HOME/Android/Sdk/ndk/26.3.11579264

# 2. Собрать Go AAR + Android APK одной командой
cd go-freenet
bash scripts/build-release-apk.sh

# Только AAR (без Android Studio)
bash scripts/build-android.sh
```

### gomobile API

```kotlin
// Инициализация (Kotlin, после сборки AAR)
val engine = Mobile.newFreenetEngine()

// Запуск SOCKS5 прокси
engine.start(1080)

// Запуск полного VPN режима (SOCKS5 + TUN форвардер)
engine.startVPN(tunFd, 1080, protector)

// Управление стратегией
engine.setStrategy("auto")  // auto / split / tlsrec / combined / none

// Статистика (JSON)
engine.getStats()  // {"active":2,"total":47,"bytes_in":102400,...}

// Остановка
engine.stop()
```

---

## Реализованные стратегии обхода DPI

| Стратегия | Описание | Требования | Эффективность |
|-----------|----------|------------|---------------|
| **split** | TCP фрагментация ClientHello на позиции SNI | нет | Средняя |
| **disorder** | Перестановка сегментов head/tail | нет | Средняя |
| **tlsrec** | TLS record layer splitting (2 TLS записи) | нет | Высокая |
| **fake** | Decoy пакет (TTL или bad checksum) + split | CAP_NET_RAW | Высокая |
| **combined** | fake + tlsrec — максимальный эффект | CAP_NET_RAW | Максимальная |
| **quic** | QUIC Initial фрагментация (UDP 443) | нет | Средняя |
| **auto** | Авто-тест всех стратегий, выбор лучшей | — | Адаптивная |
| **none** | Без обхода (для отладки) | — | — |

---

## CI/CD — GitHub Actions

Автоматическая сборка при каждом push в `master`, `main`, `neo/**`:

| Платформа | Артефакт |
|-----------|---------|
| linux/amd64 | `freenet-linux-amd64` |
| linux/arm64 | `freenet-linux-arm64` |
| linux/armv7 | `freenet-linux-armv7` |
| windows/amd64 | `freenet-windows-amd64.exe` |
| **Android AAR** | `mobile.aar` ✨ NEW |
| **Android APK** | `freenet-debug.apk` ✨ NEW |

---

## Как запустить

```bash
# Docker (рекомендуется, одна команда)
cd go-freenet && docker compose up -d
# → http://localhost:8080  (веб UI)
# → 127.0.0.1:1080        (SOCKS5)

# Напрямую (Linux/Windows)
go build -o freenet ./cmd/freenet
./freenet -web :8080

# Android APK
bash scripts/build-release-apk.sh
# → android/app/build/outputs/apk/debug/app-debug.apk
```
