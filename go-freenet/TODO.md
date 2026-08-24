# Что нужно сделать — FreeNet Go

## Приоритет: Высокий 🔴

### 1. Fake packets через raw sockets (Linux)
**Что:** Отправка фейковых TCP пакетов с неправильным checksum/TTL через `SOCK_RAW`.
Это самый эффективный метод — DPI обрабатывает фейк и путается, сервер игнорирует.

**Как:**
```go
// Linux: открыть raw socket
fd, _ := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_TCP)
// Собрать IP+TCP заголовок с неправильным TTL
// Отправить до реального ClientHello
```
**Файл:** `internal/bypass/fake.go`

---

### 2. ECH (Encrypted Client Hello)
**Что:** Шифрует SNI в TLS 1.3 handshake. DPI не видит какой сайт вы открываете.
Поддерживается Cloudflare, многими CDN. Самая современная техника 2025–2026.

**Как:**
- Запросить ECH конфиг сервера через DNS (`HTTPS` запись)
- Применить в ClientHello через `crypto/tls` с `ECHConfig`
- Go 1.23+ поддерживает ECH нативно

**Файл:** `internal/bypass/ech.go`

---

### 3. nfqueue интеграция (Linux)
**Что:** Интеграция с Linux netfilter queue — как в оригинальном zapret2.
Позволяет модифицировать пакеты на уровне ядра без SOCKS5 прокси.

**Библиотека:** `github.com/florianl/go-nfqueue`

**Преимущество:** Работает для ВСЕХ приложений автоматически, без настройки прокси.

**Файл:** `internal/proxy/nfqueue.go`

---

### 4. WinDivert интеграция (Windows)
**Что:** Обёртка для `winws.exe` (уже есть в репо `nfq2/windows/`).
Позволяет перехватывать и модифицировать пакеты на Windows без SOCKS5.

**Как:**
```go
// CGO обёртка для windivert.h
// Или: запуск winws.exe как дочерний процесс с управлением через Go
```
**Файл:** `internal/proxy/windivert.go`

---

## Приоритет: Средний 🟡

### 5. Android APK
**Что:** Нативное Android приложение с VpnService (без root).

**Технологии:**
- `gomobile bind` → `.aar` библиотека из `internal/bypass`
- Android Studio / Kotlin / Jetpack Compose
- gVisor netstack + tun2socks для захвата трафика
- Per-app фильтрация (обходить только нужные приложения)

**Структура:**
```
android/
├── app/                    # Kotlin/Compose UI
│   └── src/main/
│       ├── DpiBypassVpnService.kt
│       └── MainActivity.kt
└── freenet-core/           # gomobile AAR
```

**Команда сборки:**
```bash
gomobile bind -target android/arm64 -o android/app/libs/freenet.aar ./internal/bypass
```

---

### 6. Windows .exe установщик
**Что:** Полноценный Windows установщик с GUI.

**Компоненты:**
- Go приложение с системным треем (`github.com/getlantern/systray`)
- Встроенный `winws.exe` + WinDivert64.dll
- NSIS скрипт → `freenet-setup.exe`
- Установка как Windows Service (`golang.org/x/sys/windows/svc`)

**Структура:**
```
windows/
├── main_windows.go         # точка входа с systray
├── service.go              # Windows Service
├── windivert.go            # CGO обёртка
└── installer/
    └── freenet.nsi         # NSIS скрипт
```

---

### 7. GitHub Actions CI/CD
**Что:** Автоматическая сборка и релизы.

**Файл:** `.github/workflows/build.yml`

```yaml
# Кросс-компиляция:
# - linux/amd64  → freenet-linux-amd64
# - linux/arm64  → freenet-linux-arm64
# - windows/amd64 → freenet-windows-amd64.exe
# - android/arm64 → freenet.apk
```

---

### 8. Антизапрет список
**Что:** Интеграция с `antizapret.prostovpn.com` как альтернатива antifilter.
antizapret умнее: группирует IP по подсетям, меньше правил.

**Файл:** `ipset/get_antizapret.sh` уже есть в zapret2 — нужно портировать на Go.

---

## Приоритет: Низкий 🟢

### 9. Telegram бот
**Что:** Управление FreeNet через Telegram.

**Команды:**
```
/status  — включён/выключен
/on      — включить
/off     — выключить
/strategy split — сменить стратегию
/stats   — статистика
```

**Библиотека:** `github.com/go-telegram-bot-api/telegram-bot-api`

---

### 10. Unit тесты
**Что:** Покрытие тестами ключевых компонентов.

**Приоритет тестирования:**
- `bypass/tls_test.go` — парсинг ClientHello на реальных дампах
- `bypass/split_test.go` — проверка позиции фрагментации
- `bypass/hostlist_test.go` — wildcard matching
- `logs/ring_test.go` — кольцевой буфер, подписчики

---

### 11. OpenWrt поддержка
**Что:** Установка на роутеры OpenWrt (как в оригинальном zapret2).

**Как:**
- Кросс-компиляция под MIPS/ARM: `GOARCH=mips GOOS=linux`
- init.d скрипт для OpenWrt
- Размер бинарника критичен (нужен strip + UPX)

---

## Быстрый старт для контрибьюторов

```bash
# Клонировать
git clone https://github.com/mintfary-oss/zapret2-may.git
cd zapret2-may/go-freenet

# Запустить
go run ./cmd/freenet -web :8080

# Тесты
go test ./...

# Собрать для всех платформ
GOOS=linux   GOARCH=amd64 go build -o dist/freenet-linux-amd64   ./cmd/freenet
GOOS=linux   GOARCH=arm64 go build -o dist/freenet-linux-arm64   ./cmd/freenet
GOOS=windows GOARCH=amd64 go build -o dist/freenet-windows.exe   ./cmd/freenet
```
