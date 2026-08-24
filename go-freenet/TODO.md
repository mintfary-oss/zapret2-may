# Что нужно сделать — FreeNet Go

## Phase 2 статус: ✅ ЗАВЕРШЕНА
## Phase 3 статус: ✅ ЗАВЕРШЕНА (Android APK)

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

## Phase 7 — Android улучшения 🔵

- [ ] `xjasonlyu/tun2socks/v2` — заменить минимальный TCP стек на production-grade gVisor
- [ ] IPv6 поддержка в TUN форвардере
- [ ] UDP/QUIC поддержка (DNS, HTTP/3)
- [ ] Per-app фильтрация (bypass только для выбранных приложений)
- [ ] Google Play / F-Droid packaging

---

## Быстрый старт для контрибьюторов

```bash
# Клонировать
git clone https://github.com/mintfary-oss/zapret2-may.git
cd zapret2-may/go-freenet

# Запустить (Linux/Docker)
go run ./cmd/freenet -web :8080

# Android APK
bash scripts/build-release-apk.sh

# Тесты
go test ./...

# Собрать для всех платформ
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -o dist/freenet-linux-amd64   ./cmd/freenet
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -o dist/freenet-linux-arm64   ./cmd/freenet
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/freenet-windows.exe   ./cmd/freenet
```
