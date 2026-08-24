# Задачи — FreeNet Go

## Текущий статус

| Phase | Название | Статус |
|-------|----------|--------|
| 1 | Ядро + Docker + Linux | ✅ Завершена |
| 2 | Fake packets + nfqueue + TLS record | ✅ Завершена |
| 3 | Android APK (gomobile + VpnService) | ✅ Завершена |
| 4 | Windows GUI + WinDivert | 🔄 Следующая |
| 5 | ECH + тесты + качество | 📋 В плане |
| 6 | Android улучшения | 📋 В плане |

---

## Phase 4 — Windows GUI + установщик

### 4.1 Системный трей

```go
// go get github.com/getlantern/systray
import "github.com/getlantern/systray"

func main() {
    systray.Run(onReady, onExit)
}

func onReady() {
    systray.SetTitle("FreeNet")
    systray.SetTooltip("FreeNet DPI bypass")
    mToggle := systray.AddMenuItem("Включить", "Запустить bypass")
    mWeb    := systray.AddMenuItem("Открыть UI", "http://localhost:8080")
    systray.AddSeparator()
    mQuit   := systray.AddMenuItem("Выйти", "")
    go func() {
        for {
            select {
            case <-mToggle.ClickedCh: toggleBypass()
            case <-mWeb.ClickedCh:   openBrowser("http://localhost:8080")
            case <-mQuit.ClickedCh:  systray.Quit()
            }
        }
    }()
}
```

### 4.2 WinDivert интеграция

WinDivert DLL уже есть в репо (`nfq2/windows/`). Нужна CGO обёртка:

```go
//go:build windows
// #cgo LDFLAGS: -lWinDivert -L.
// #include "windivert.h"
import "C"

func interceptPacket() {
    handle := C.WinDivertOpen("ip and tcp.DstPort == 443", C.WINDIVERT_LAYER_NETWORK, 0, 0)
    // ...
}
```

### 4.3 NSIS установщик

```nsi
; freenet-setup.nsi
Name "FreeNet"
OutFile "freenet-setup.exe"
InstallDir "$PROGRAMFILES64\FreeNet"
RequestExecutionLevel admin

Section "Install"
  SetOutPath "$INSTDIR"
  File "freenet.exe"
  File "WinDivert64.dll"
  File "WinDivert64.sys"
  CreateShortCut "$SMPROGRAMS\FreeNet.lnk" "$INSTDIR\freenet.exe"
  WriteUninstaller "$INSTDIR\uninstall.exe"
  ; Установить как службу Windows
  ExecWait '"$INSTDIR\freenet.exe" -install'
SectionEnd

Section "Uninstall"
  ExecWait '"$INSTDIR\freenet.exe" -uninstall'
  Delete "$INSTDIR\*.*"
  RMDir "$INSTDIR"
SectionEnd
```

---

## Phase 5 — ECH (Encrypted Client Hello)

Самая современная техника 2026. Шифрует SNI в расширении ClientHello TLS 1.3.
Провайдер не видит какой сайт запрашивается — только адрес IP сервера.

```go
// Go 1.23+ поддерживает ECH нативно
import "crypto/tls"

// Получить ECH config из DNS HTTPS записи
echConfig, _ := getECHConfigFromDNS("cloudflare.com")

tlsConfig := &tls.Config{
    EncryptedClientHelloConfigList: echConfig,
    MinVersion: tls.VersionTLS13,
}

conn, _ := tls.Dial("tcp", "1.1.1.1:443", tlsConfig)
```

Требования:
- Go 1.23+
- DNS HTTPS запись сервера (Cloudflare, Google, многие CDN)
- Fallback на split/tlsrec для серверов без ECH

---

## Phase 5 — Unit тесты

Приоритет тестирования:

```go
// bypass/tls_test.go
func TestParseClientHello(t *testing.T) {
    // Тест на реальных TLS дампах с youtube.com, vk.com
}

// bypass/split_test.go
func TestSplitAtSNI(t *testing.T) {
    // Проверить что split происходит точно перед SNI offset
}

// bypass/hostlist_test.go
func TestHostlistWildcard(t *testing.T) {
    // "www.youtube.com" должен матчить "youtube.com"
}

// bypass/autodetect_test.go
func TestAutoDetect(t *testing.T) {
    // Mock HTTP сервер, проверить что auto выбирает рабочую стратегию
}

// logs/ring_test.go
func TestRingBufferFIFO(t *testing.T) {
    // Проверить FIFO порядок и capacity
}
```

---

## Phase 6 — Android улучшения

- [ ] Заменить минимальный TCP стек на `xjasonlyu/tun2socks/v2` (gVisor-based)
- [ ] IPv6 поддержка в TUN форвардере
- [ ] UDP/QUIC через TUN (DNS, HTTP/3)
- [ ] Per-app фильтрация (только выбранные приложения через VPN)
- [ ] Виджет на рабочем столе (вкл/выкл без открытия приложения)
- [ ] Автоматическое обновление списков блокировок в фоне
- [ ] F-Droid манифест для публикации

---

## Быстрый старт для разработчика

```bash
# Клонировать репозиторий
git clone https://github.com/mintfary-oss/zapret2-may.git
cd zapret2-may/go-freenet

# Запустить на Linux/Docker
go run ./cmd/freenet -web :8080

# Запустить тесты
go test ./...

# Кросс-компиляция
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -o dist/freenet-linux-amd64   ./cmd/freenet
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -o dist/freenet-linux-arm64   ./cmd/freenet
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/freenet-windows-amd64.exe ./cmd/freenet

# Android AAR (требует Go + Android NDK)
export ANDROID_NDK_HOME=$HOME/Android/Sdk/ndk/26.3.11579264
bash scripts/build-android.sh

# Создать релиз
git tag freenet-v1.1.0
git push origin freenet-v1.1.0
# → GitHub Actions автоматически соберёт все платформы и опубликует релиз
```
