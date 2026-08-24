# Что нужно сделать — FreeNet Go

## Phase 2 статус: ✅ ЗАВЕРШЕНА

Выполнено:
- ✅ Fake packets через raw sockets (TTL + bad checksum)
- ✅ nfqueue интеграция (kernel-level, все приложения)
- ✅ TLS record layer splitting (tlsrec + combined)
- ✅ Windows cross-compile (freenet-windows-amd64.exe)
- ✅ GitHub Actions CI/CD (linux/amd64, arm64, armv7, windows/amd64)

---

## Phase 3 — Android APK 🔴

### Задача
Нативное Android приложение с VpnService (без root).

### Технологии
- `gomobile bind` → `.aar` библиотека из `internal/bypass`
- Android Studio / Kotlin / Jetpack Compose
- gVisor netstack + tun2socks для захвата трафика
- Per-app фильтрация

### Шаги реализации

**1. Подготовить Go core для gomobile**

```bash
# Установить gomobile
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init

# Сборка AAR
gomobile bind \
  -target android/arm64,android/arm \
  -androidapi 26 \
  -javapkg com.freenet.bypass \
  -o android/app/libs/freenet.aar \
  ./internal/bypass
```

**2. Создать Android проект** (`android/`)
```
android/
├── app/
│   └── src/main/
│       ├── AndroidManifest.xml    # VpnService permission
│       ├── java/com/freenet/
│       │   ├── MainActivity.kt    # UI — большая кнопка
│       │   ├── VpnService.kt      # VpnService + tun2socks
│       │   └── BypassEngine.kt    # обёртка над freenet.aar
│       └── res/layout/
└── build.gradle
```

**3. VpnService логика**
```kotlin
class FreenetVpnService : VpnService() {
    override fun onStartCommand(...): Int {
        val builder = Builder()
            .addAddress("10.0.0.1", 24)
            .addRoute("0.0.0.0", 0)       // весь трафик
            .setMtu(1500)
        val vpnFd = builder.establish()
        // tun2socks: преобразует TUN → SOCKS5 → bypass engine
        startTun2Socks(vpnFd)
    }
}
```

---

## Phase 4 — Windows GUI + установщик 🟡

### Задача
Нативный Windows .exe с системным треем и установщиком.

### Шаги реализации

**1. Системный трей**
```go
// Библиотека: github.com/getlantern/systray
import "github.com/getlantern/systray"

func main() {
    systray.Run(onReady, onExit)
}

func onReady() {
    systray.SetIcon(iconData)
    systray.SetTitle("FreeNet")
    mToggle := systray.AddMenuItem("Включить", "")
    mQuit   := systray.AddMenuItem("Выйти", "")
    // ...
}
```

**2. WinDivert интеграция** (пакетный перехват без SOCKS5)

Библиотека WinDivert уже есть в репо (`nfq2/windows/`). Нужна CGO обёртка:
```go
//go:build windows
// #include "windivert.h"
import "C"
```

**3. NSIS установщик**
```nsi
; freenet-setup.nsi
Name "FreeNet"
OutFile "freenet-setup.exe"
InstallDir "$PROGRAMFILES64\FreeNet"

Section "Install"
  SetOutPath "$INSTDIR"
  File "freenet.exe"
  File "WinDivert64.dll"
  File "WinDivert64.sys"
  CreateShortCut "$SMPROGRAMS\FreeNet.lnk" "$INSTDIR\freenet.exe"
SectionEnd
```

---

## Phase 5 — Качество и тесты 🟢

### Unit тесты

Приоритет тестирования:

```go
// bypass/tls_test.go
func TestParseClientHello(t *testing.T) {
    // Тест на реальных TLS дампах с youtube.com, vk.com и т.д.
}

// bypass/split_test.go
func TestSplitPosition(t *testing.T) {
    // Проверить что split происходит точно перед SNI
}

// bypass/hostlist_test.go
func TestHostlistWildcard(t *testing.T) {
    // "www.youtube.com" должен матчить "youtube.com"
}

// logs/ring_test.go
func TestRingBuffer(t *testing.T) {
    // Проверить FIFO порядок, capacity, subscribers
}
```

### Integration тесты

```go
// Запустить mock DPI server, проверить что split стратегия обходит его
func TestBypassMockDPI(t *testing.T) { ... }
```

---

## Phase 6 — ECH (Encrypted Client Hello) 🟢

Самая передовая техника 2026. Шифрует SNI в TLS 1.3.

```go
// Go 1.23+ поддерживает ECH нативно через crypto/tls
tlsConfig := &tls.Config{
    EncryptedClientHelloConfigList: echConfig,
}
```

Требует:
- DNS HTTPS запись для получения ECH конфига сервера
- Сервер должен поддерживать ECH (Cloudflare, многие CDN)

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
make build   # см. Makefile (скоро)

# или вручную:
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -o dist/freenet-linux-amd64   ./cmd/freenet
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -o dist/freenet-linux-arm64   ./cmd/freenet
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/freenet-windows.exe   ./cmd/freenet
```
