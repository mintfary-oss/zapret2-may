# Ошибки и как они были исправлены

## В процессе разработки go-freenet

---

### ERR-001: `syscall.Getsockopt undefined`

**Файл:** `internal/proxy/transparent.go`

**Ошибка:**
```
internal/proxy/transparent.go:67:21: undefined: syscall.Getsockopt
```

**Причина:** В Go `syscall.Getsockopt` не доступен напрямую на всех платформах. Стандартный пакет `syscall` не экспортирует эту функцию для Linux/arm64.

**Исправление:** Добавлена зависимость `golang.org/x/sys/unix` и использован `unix.Syscall6` с `unix.SYS_GETSOCKOPT`:
```go
//go:build linux
import "golang.org/x/sys/unix"

unix.Syscall6(unix.SYS_GETSOCKOPT, fd, unix.SOL_IP, soOriginalDst, ...)
```
Также добавлен build tag `//go:build linux` — файл компилируется только на Linux.

---

### ERR-002: Конфликт типов `StatsSnapshot` и `ProbeResult`

**Файл:** `internal/web/ui.go` ↔ `internal/proxy/server.go`

**Ошибка:** Интерфейс `web.Controller` требовал `web.StatsSnapshot`, а `proxy.Server` возвращал `proxy.StatsSnapshot` — разные типы, Go не принимает.

**Причина:** Типы были определены в двух разных пакетах, интерфейс не совпадал.

**Исправление:** Создан общий пакет `internal/types/types.go` с общими структурами:
```go
package types

type StatsSnapshot struct { ... }
type ProbeResult struct { ... }
```
Оба пакета (`web` и `proxy`) теперь импортируют из `types` — цикличных импортов нет.

---

### ERR-003: `gofmt` форматирование

**Файлы:** `internal/logs/ring.go`, `internal/proxy/server.go`, `internal/proxy/socks5.go`

**Ошибка:** `gofmt -l .` выдавал список файлов с неправильным форматированием.

**Исправление:** `gofmt -w .` — автоматически форматирует все файлы.

---

### ERR-004: Push authentication failed

**Ошибка:**
```
remote: Invalid username or token. Password authentication is not supported.
fatal: Authentication failed for 'https://github.com/mintfary-oss/zapret2-may/'
```

**Причина:** Системный git credential helper не имел доступа к репозиторию `mintfary-oss/zapret2-may` — репозиторий не подключён к Pulumi Cloud GitHub App.

**Исправление:** Пользователь предоставил Personal Access Token. После использования токен был удалён из git remote URL.

---

### ERR-005: `gh pr create` — missing scope `read:org`

**Ошибка:**
```
error validating token: missing required scope 'read:org'
```

**Причина:** GitHub CLI (`gh`) требует scope `read:org` для аутентификации, которого у токена не было.

**Исправление:** PR создан напрямую через GitHub REST API:
```bash
curl -X POST \
  -H "Authorization: token ..." \
  https://api.github.com/repos/mintfary-oss/zapret2-may/pulls \
  -d '{"title": "...", "head": "...", "base": "master"}'
```

---

### ERR-006: QUIC bypass — ограничение без raw sockets

**Описание:** Полноценный QUIC bypass (инъекция фейковых QUIC Initial пакетов) требует raw UDP сокетов и прав root. Без них нельзя послать пакет с произвольными полями QUIC.

**Статус:** Реализован partial bypass — фрагментация первого QUIC Initial датаграмма. Полный bypass через nfqueue (Linux) или WinDivert (Windows) — в плане.

---

### ERR-007: Disorder атака — ограничение на уровне приложения

**Описание:** Настоящая disorder атака (TCP сегменты не по порядку) невозможна без raw сockets — ядро ОС всегда отправляет сегменты в порядке `write()`.

**Статус:** Реализован best-effort вариант: head/tail с микро-паузой. Для настоящего disorder нужен Linux nfqueue (как в оригинальном zapret2).
