# Ошибки и исправления — FreeNet Go

Полный журнал ошибок, найденных в процессе разработки, и способы их решения.

---

## Ошибки компиляции Go

### ERR-001: `syscall.Getsockopt undefined`

**Файл:** `internal/proxy/transparent.go`

**Ошибка:**
```
internal/proxy/transparent.go:67:21: undefined: syscall.Getsockopt
```

**Причина:** `syscall.Getsockopt` не доступен на всех платформах через стандартный пакет.

**Исправление:** Заменён на `unix.Syscall6` из `golang.org/x/sys/unix` с build tag `//go:build linux`:
```go
//go:build linux
import "golang.org/x/sys/unix"
unix.Syscall6(unix.SYS_GETSOCKOPT, fd, unix.SOL_IP, soOriginalDst, ...)
```

---

### ERR-002: Конфликт типов `StatsSnapshot` и `ProbeResult`

**Файлы:** `internal/web/ui.go` ↔ `internal/proxy/server.go`

**Ошибка:** Интерфейс `web.Controller` требовал `web.StatsSnapshot`, `proxy.Server`
возвращал `proxy.StatsSnapshot` — разные типы, несовместимые в Go.

**Исправление:** Создан общий пакет `internal/types/types.go`:
```go
package types
type StatsSnapshot struct { ... }
type ProbeResult struct { ... }
```
Оба пакета импортируют из `types` — цикличных импортов нет.

---

### ERR-003: Форматирование `gofmt`

**Файлы:** `internal/logs/ring.go`, `internal/proxy/server.go`, `internal/proxy/socks5.go`

**Ошибка:** CI шаг `gofmt -l .` выдавал список файлов с нарушением форматирования.

**Исправление:** `gofmt -w .` — автоматическое форматирование.

---

## Ошибки аутентификации

### ERR-004: Push authentication failed

**Ошибка:**
```
remote: Invalid username or token.
fatal: Authentication failed for 'https://github.com/mintfary-oss/zapret2-may/'
```

**Причина:** Репозиторий `mintfary-oss` не подключён к Pulumi Cloud GitHub App.

**Исправление:** Пользователь предоставил Personal Access Token (PAT).
После использования токен удалён из git remote URL.

---

### ERR-005: `gh pr create` — missing scope `read:org`

**Ошибка:**
```
error validating token: missing required scope 'read:org'
```

**Причина:** GitHub CLI требует scope `read:org`, которого не было у PAT.

**Исправление:** PR создан напрямую через GitHub REST API:
```bash
curl -X POST -H "Authorization: token ..." \
  https://api.github.com/repos/mintfary-oss/zapret2-may/pulls \
  -d '{"title":"...","head":"neo/...","base":"master"}'
```

---

## Ограничения реализации

### ERR-006: QUIC bypass без raw sockets

**Описание:** Полноценная инъекция фейковых QUIC Initial пакетов требует `SOCK_RAW` + `CAP_NET_RAW`.

**Статус:** Реализована фрагментация первого QUIC датаграмма. Полный bypass — через nfqueue (Linux).

---

### ERR-007: Disorder атака — ограничение ОС

**Описание:** Настоящий disorder (TCP сегменты не по порядку) невозможен без raw sockets —
ядро всегда отправляет сегменты в порядке `write()`.

**Статус:** Best-effort вариант: head/tail с микро-паузой. Настоящий disorder — через nfqueue.

---

## Ошибки CI/CD — GitHub Actions

### ERR-CI-01: `use of internal package not allowed` (gomobile)

**Workflow job:** Android APK → Build Android AAR

**Ошибка:**
```
gobind/go_mobilemain.go:18:2: use of internal package
  github.com/mintfary-oss/freenet/internal/mobile not allowed
```

**Причина:** В Go пакеты в директории `internal/` могут импортироваться только кодом
из того же модуля. `gomobile` — внешний инструмент, поэтому импорт запрещён.

**Исправление:** Пакет перемещён из `internal/mobile/` → `mobile/` (публичный путь).
Обновлены workflow и скрипты сборки.

**Коммит:** `fix(android): move mobile/ out of internal/ so gomobile bind can access it`

---

### ERR-CI-02: `android.useAndroidX property is not enabled`

**Workflow job:** Android APK → Build Android APK (Gradle)

**Ошибка:**
```
Execution failed for task ':app:checkDebugAarMetadata'.
> Configuration contains AndroidX dependencies, but the android.useAndroidX
  property is not enabled. Set android.useAndroidX=true in gradle.properties.
```

**Причина:** Отсутствовал файл `android/gradle.properties`. Без него Gradle
не включает режим совместимости с AndroidX, обязательный для Jetpack Compose.

**Исправление:** Создан `android/gradle.properties`:
```properties
android.useAndroidX=true
kotlin.code.style=official
android.nonTransitiveRClass=true
org.gradle.jvmargs=-Xmx2048m
```

**Коммит:** `fix(android): add gradle.properties with android.useAndroidX=true`

---

### ERR-CI-03: `Unresolved reference 'and'` (Kotlin)

**Workflow job:** Android APK → Build Android APK (compileDebugKotlin)

**Ошибка:**
```
e: PacketForwarder.kt:137:28 Unresolved reference 'and'.
e: PacketForwarder.kt:138:28 Unresolved reference 'and'.
```

**Причина:** В Kotlin `and` как инфиксный оператор определён для `Int` и `Long`,
но не для `Byte`. Строки вида `(flags and FLAG_FIN)` где оба операнда `Byte`
вызывают ошибку компиляции.

**Исправление:** TCP-флаги изменены с `Byte` на `Int`, операция через `.toInt()`:
```kotlin
// До:
private const val FLAG_FIN: Byte = 0x01
val isFin = (flags and FLAG_FIN) != 0.toByte()

// После:
private const val FLAG_FIN: Int = 0x01
val fi    = flags.toInt() and 0xFF
val isFin = (fi and FLAG_FIN) != 0
```

**Коммит:** `fix(android): use Int for TCP flag constants, fix Byte.and infix Kotlin error`

---

### ERR-CI-04: `Pattern 'dist/*' does not match any files`

**Workflow job:** Create GitHub Release (softprops/action-gh-release)

**Ошибка:**
```
🤔 Pattern 'dist/*' does not match any files.
🤔 dist/* does not include a valid file.
```

**Причина:** `softprops/action-gh-release@v2` разрешает путь в параметре `files:`
относительно `$GITHUB_WORKSPACE` (корень репозитория), игнорируя `defaults.run.working-directory`.
Файлы находились в `go-freenet/dist/`, а workflow искал в `dist/`.

**Диагностика:** Команда `ls -lh dist/` (которая выполняется в `go-freenet/`) показывала
все файлы (86 MB), но action не видел их из-за разницы в базовых директориях.

**Исправление:** Изменён путь в `files:`:
```yaml
# До:
files: dist/*

# После:
files: go-freenet/dist/*
```

**Коммит:** `fix(ci): use workspace-relative path for release files`

---

## Статистика ошибок

| Категория | Количество | Статус |
|-----------|------------|--------|
| Ошибки компиляции Go | 3 | ✅ Исправлены |
| Ошибки аутентификации | 2 | ✅ Исправлены |
| Ограничения реализации | 2 | 🔄 В плане |
| Ошибки CI/CD | 4 | ✅ Исправлены |
| **Итого** | **11** | |
