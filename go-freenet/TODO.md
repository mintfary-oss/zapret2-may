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
| 8 | F-Droid + качество | — | 🔄 Следующая |
| 9 | Telegram бот | — | 📋 В плане |

---

## Phase 8 — F-Droid + качество

### 8.1 F-Droid манифест

```yaml
# metadata/com.freenet.vpn.yml
Categories:
  - Security
License: MIT
SourceCode: https://github.com/mintfary-oss/zapret2-may
IssueTracker: https://github.com/mintfary-oss/zapret2-may/issues
Builds:
  - versionName: 1.6.0
    versionCode: 160
    commit: freenet-v1.6.0
    subdir: go-freenet/android
    gradle:
      - yes
    prebuild:
      - bash ../scripts/build-android.sh
```

### 8.2 Android integration tests

```kotlin
// android/app/src/androidTest/VpnServiceTest.kt
@RunWith(AndroidJUnit4::class)
class VpnServiceTest {
    @Test fun testVpnServiceStartStop() {
        val ctx = ApplicationProvider.getApplicationContext<Context>()
        // Проверяем что сервис запускается и останавливается без краша
    }
}
```

### 8.3 OpenWrt init.d скрипт

```sh
#!/bin/sh /etc/rc.common
START=99
STOP=10
USE_PROCD=1
PROG=/usr/bin/freenet

start_service() {
    procd_open_instance
    procd_set_param command $PROG -web :8080 -config /etc/freenet/config.yaml
    procd_set_param respawn
    procd_close_instance
}
```

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
