# Переписка с Neo (Pulumi AI Agent)

## Сессия: Август 2026

---

### 1. Первый запрос

**Пользователь:** Дал ссылку на репозиторий `mintfary-oss/zapret2-may` и попросил создать программу
для обхода блокировок в России на телефоне, Linux, Windows, с веб-интерфейсом, Docker и т.д.

**Neo:** Изучил репозиторий, нашёл zapret2 (C/Lua движок DPI bypass). Предложил переписать ядро
на Go для кросс-платформенности и добавить Android APK через gomobile.

---

### 2. Архитектурное обсуждение

**Пользователь:** "Что ещё можно добавить, какие новинки на 2026?"

**Neo:** Провёл исследование конкурентов (GoodbyeDPI, ByeDPI, Zapret, Xray). Нашёл:
- В 2026 DPI системы используют JA3/JA4 fingerprinting и полную TCP реассемблировку
- Новая техника: **ECH** (Encrypted Client Hello) — шифрует SNI в TLS 1.3
- Конкуренты не поддерживают одновременно Linux + Windows + Android

Составил план "FreeNet Go 2026" — один Go-монорепозиторий для всех платформ.

---

### 3. Phase 1 — Ядро + Docker (ЗАВЕРШЕНА)

**Создано:** 26 файлов, 2303 строки Go.

Что было написано:
- `internal/bypass/` — все стратегии обхода DPI (split, disorder, fake, QUIC, TLS record)
- `internal/proxy/` — SOCKS5 + прозрачный прокси + nfqueue
- `internal/web/` — веб-UI с большой кнопкой + WebSocket логи
- `Dockerfile` + `docker-compose.yml` — одна команда запуска
- `scripts/install.sh` — установщик как systemd сервис

---

### 4. Phase 2 — Усиление bypass (ЗАВЕРШЕНА)

Добавлено:
- `fake.go` / `fake_linux.go` — fake packets через raw socket (TTL=4, bad checksum)
- `nfqueue.go` — перехват пакетов на уровне ядра Linux через netfilter
- `tlsrec.go` — TLS record layer splitting (2 TLS записи вместо одной)
- `combined` стратегия — fake + tlsrec одновременно
- Кросс-компиляция Windows exe
- GitHub Actions CI/CD

---

### 5. Phase 3 — Android APK (ЗАВЕРШЕНА)

**Пользователь:** "Android и Windows отдельным приложением, как Telegram — есть APK и exe"

**Neo:** Создал полный Android проект:
- `mobile/engine.go` — gomobile-bindable Go API
- `mobile/tun.go` — IPv4/TCP TUN packet forwarder
- `android/` — Kotlin/Compose Android Studio проект
- `FreenetVpnService.kt` — Android VpnService (без root)
- `MainActivity.kt` — UI с большой кнопкой, статистикой, логами
- `BootReceiver.kt` — автозапуск при загрузке

---

### 6. Авторелиз — одна кнопка скачать и установить (ЗАВЕРШЕНА)

**Пользователь:** "В веб-интерфейсе и на GitHub уже должны быть готовые установщики — скачал, нажал, установилось"

**Neo:** Реализовал:
- GitHub Actions workflow с авто-релизом при `git tag freenet-v*`
- Страница "Скачать" в веб-UI с определением ОС пользователя
- PowerShell one-liner для Windows
- bash one-liner для Linux
- Установщик как Windows Service (`freenet.exe -install`)

---

### 7. Отладка CI/CD — исправление 4 ошибок (ЗАВЕРШЕНА)

**ERR-CI-01: `use of internal package not allowed`**

Проблема: `gomobile bind` не может импортировать пакеты из `internal/`.
Исправление: Перемещён `internal/mobile/` → `mobile/` (публичный путь).

**ERR-CI-02: `android.useAndroidX property is not enabled`**

Проблема: Отсутствовал `gradle.properties` с `android.useAndroidX=true`.
Исправление: Создан `android/gradle.properties`.

**ERR-CI-03: `Unresolved reference 'and'` (Kotlin)**

Проблема: Kotlin не поддерживает `and` как инфиксный оператор для `Byte`.
Исправление: Константы TCP-флагов изменены с `Byte` на `Int`, добавлен `.toInt()`.

**ERR-CI-04: `Pattern 'dist/*' does not match any files`**

Проблема: `softprops/action-gh-release` разрешает `files:` относительно `$GITHUB_WORKSPACE`,
а не `working-directory` задания.
Исправление: Изменено `files: dist/*` → `files: go-freenet/dist/*`.

---

### 8. Итоговый результат

**Релиз freenet-v1.0.5** опубликован: https://github.com/mintfary-oss/zapret2-may/releases/tag/freenet-v1.0.5

Все платформы собираются автоматически при `git tag freenet-vX.Y.Z`:

| Файл | Платформа | Размер |
|------|-----------|--------|
| `freenet-android.apk` | Android | ~37 MB |
| `freenet-windows-amd64.exe` | Windows | ~7 MB |
| `freenet-linux-amd64` | Linux x64 | ~7 MB |
| `freenet-linux-arm64` | Linux ARM64 | ~7 MB |
| `freenet-linux-armv7` | Raspberry Pi | ~7 MB |
| `freenet-linux-amd64-installer.tar.gz` | Linux bundle | ~3 MB |
| `install.sh` | Linux one-liner | — |
| `install-windows.ps1` | Windows one-liner | — |
| `mobile.aar` | Android AAR (разработчикам) | ~15 MB |

---

### 9. Важное: токены GitHub

В ходе сессии пользователь три раза публиковал GitHub токены в открытом чате.
Все использованные токены необходимо немедленно отозвать:
https://github.com/settings/tokens
