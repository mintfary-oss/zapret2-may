# Техническая документация — FreeNet Go

## Содержание

1. [Обзор архитектуры](#1-обзор-архитектуры)
2. [Как работает обход DPI](#2-как-работает-обход-dpi)
3. [Стратегии обхода](#3-стратегии-обхода)
4. [Перехват трафика](#4-перехват-трафика)
5. [SOCKS5 прокси](#5-socks5-прокси)
6. [Веб-интерфейс](#6-веб-интерфейс)
7. [Android архитектура](#7-android-архитектура)
8. [Конфигурация](#8-конфигурация)
9. [Сборка и деплой](#9-сборка-и-деплой)
10. [CI/CD pipeline](#10-cicd-pipeline)
11. [Безопасность](#11-безопасность)

---

## 1. Обзор архитектуры

FreeNet Go — SOCKS5 прокси с модулем обхода DPI, написанный на Go.
Работает на уровне пользователя (userspace), не требует изменений ядра.

```
Браузер / Приложение
        │
        │ SOCKS5 (127.0.0.1:1080)
        ▼
┌───────────────────────────────┐
│       SOCKS5 прокси           │
│   internal/proxy/socks5.go    │
└───────────┬───────────────────┘
            │ TCP соединение к target
            ▼
┌───────────────────────────────┐
│      DPI Bypass Engine        │
│   internal/bypass/engine.go   │
│                               │
│  split / disorder / tlsrec /  │
│  fake / combined / quic / auto│
└───────────┬───────────────────┘
            │ модифицированные TCP сегменты
            ▼
        Интернет (мимо ТСПУ)
```

Для прозрачного режима (все приложения без настройки SOCKS5):

```
Любое приложение
        │
        │ обычный TCP (порт 443 и др.)
        ▼
  iptables REDIRECT
  (порт 443 → 1081)
        │
        ▼
┌───────────────────────────────┐
│   Прозрачный прокси           │
│  internal/proxy/transparent.go│
│  (SO_ORIGINAL_DST для адреса) │
└───────────┬───────────────────┘
            │
            ▼
      DPI Bypass Engine
```

---

## 2. Как работает обход DPI

### Что такое DPI (Deep Packet Inspection)

Российская система ТСПУ (Технические средства противодействия угрозам) использует
DPI-оборудование (Эриксон SORM, отечественные разработки) для:

1. **Анализа TLS ClientHello** — в незашифрованном заголовке TLS виден SNI (Server Name Indication) — домен, к которому подключается клиент
2. **Блокировки по SNI** — если SNI в списке блокировки (youtube.com, instagram.com и т.д.) — соединение сбрасывается
3. **QUIC fingerprinting** — аналогично для HTTP/3 (QUIC протокол)

### Почему обычный VPN работает

VPN шифрует весь трафик до выхода из России. DPI видит только зашифрованный туннель, не может определить содержимое.

**Недостатки VPN:** Сам VPN может быть заблокирован. Нужен внешний сервер.

### Как работает FreeNet (без VPN)

FreeNet манипулирует TCP-сегментами так, чтобы DPI не смог собрать полный TLS ClientHello и определить SNI, хотя реальный сервер получает все данные корректно.

**Ключевой принцип:** Отправляем данные "не по порядку" или дополненные "мусором" с точки зрения DPI, но корректные для TCP-стека на обоих концах соединения.

---

## 3. Стратегии обхода

### 3.1 Split (TCP фрагментация)

**Файл:** `internal/bypass/split.go`

**Принцип:**
```
Обычно:
[IP][TCP] [TLS ClientHello: версия + SNI + шифры]
                              ↑
                           DPI видит SNI → БЛОК

Split:
[IP][TCP] [TLS ClientHello часть 1: до SNI]
[IP][TCP] [TLS ClientHello часть 2: SNI + остаток]
                              ↑
               DPI видит только кусок → не может определить SNI
```

**Реализация:**
1. Перехватываем первый `write()` после TCP handshake
2. Парсим TLS ClientHello (`internal/bypass/tls.go`) — находим offset SNI
3. Отправляем два TCP сегмента: `data[:splitPos]` и `data[splitPos:]`

```go
func (s *SplitStrategy) Apply(conn net.Conn, data []byte) error {
    pos := findSNIOffset(data)  // из tls.go
    if pos <= 0 || pos >= len(data) {
        _, err := conn.Write(data)
        return err
    }
    if _, err := conn.Write(data[:pos]); err != nil {
        return err
    }
    _, err := conn.Write(data[pos:])
    return err
}
```

**Ограничения:** Современные DPI умеют реассемблировать TCP сегменты. Работает не против всех провайдеров.

---

### 3.2 TLS Record Splitting

**Файл:** `internal/bypass/tlsrec.go`

**Принцип:** Разбиваем один TLS Record на два перед отправкой.

Структура TLS Record:
```
[ContentType:1][Version:2][Length:2][Data:N]
```

Split на уровне TLS Record:
```
[ContentType:1][Version:2][Length:splitPos][Data[:splitPos]]
[ContentType:1][Version:2][Length:rest][Data[splitPos:]]
```

DPI ожидает один Record, получает два — не может правильно разобрать ClientHello.

---

### 3.3 Fake Packets

**Файл:** `internal/bypass/fake_linux.go`

**Принцип:** Перед настоящим TLS ClientHello отправляем поддельный пакет с TTL=4.

```
TTL=4:   [Fake TLS ClientHello] → умирает через 4 хопа, до сервера не доходит
           ↑ DPI видит этот пакет первым, анализирует его
TTL=128: [Реальный TLS ClientHello] → доходит до сервера
           ↑ DPI запутан, пытается реассемблировать "неправильный" поток
```

DPI-система обрабатывает весь трафик, включая умершие пакеты. Видит fake ClientHello (с мусорным SNI или invalid checksum), считает соединение завершённым или невалидным, реальные данные пропускает.

**Требования:** `CAP_NET_RAW` (root или `setcap cap_net_raw+ep freenet`).

**Реализация (Linux):**
```go
//go:build linux
func sendFake(dstIP net.IP, dstPort uint16, payload []byte) error {
    fd, _ := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_RAW)
    // Строим IP пакет с TTL=4
    pkt := buildIPPacket(dstIP, dstPort, 4, fakePayload)
    return syscall.Sendto(fd, pkt, 0, &addr)
}
```

---

### 3.4 QUIC Bypass

**Файл:** `internal/bypass/quic.go`

**Принцип:** QUIC (HTTP/3) использует UDP порт 443. Первый датаграмм — QUIC Initial — содержит SNI в открытом виде (до установки шифрования).

Фрагментируем первый датаграмм QUIC Initial на несколько UDP пакетов. DPI не может восстановить полный QUIC Initial для анализа SNI.

---

### 3.5 Auto-Detect

**Файл:** `internal/bypass/autodetect.go`

Алгоритм:
1. Пробуем подключиться к `detectHost` (по умолчанию: `youtube.com:443`) без bypass
2. Если успех — bypass не нужен, используем `none`
3. Если ошибка/таймаут — пробуем стратегии по очереди: `split` → `tlsrec` → `combined`
4. Первая успешная стратегия сохраняется как текущая

```go
func (a *AutoDetect) Probe() string {
    for _, strategy := range []string{"split", "tlsrec", "combined", "fake"} {
        if a.testStrategy(strategy) {
            return strategy
        }
    }
    return "split" // fallback
}
```

---

### 3.6 Hostlist

**Файл:** `internal/bypass/hostlist.go`

Bypass применяется только к доменам из списка заблокированных. Для незаблокированных сайтов трафик идёт напрямую без модификации — снижается нагрузка и latency.

Источники списков:
- `antifilter.download/list/domains.lst` — 800K+ доменов
- Локальный файл `domains.lst`
- Вручную добавленные домены в конфиге

---

## 4. Перехват трафика

### 4.1 SOCKS5 (ручная настройка)

Пользователь настраивает браузер или приложение использовать SOCKS5 `127.0.0.1:1080`.

Плюсы: Работает везде, не нужны права root.
Минусы: Нужно настраивать каждое приложение отдельно.

### 4.2 Прозрачный прокси (Linux)

Все TCP соединения автоматически перехватываются через iptables:

```bash
# Перехватываем весь исходящий TCP трафик на порт 443
iptables -t nat -A OUTPUT -p tcp --dport 443 -j REDIRECT --to-ports 1081
```

FreeNet слушает на порту 1081 и использует `SO_ORIGINAL_DST` для получения оригинального адреса назначения (до редиректа).

```go
// transparent.go
func getOriginalDst(conn net.Conn) (*net.TCPAddr, error) {
    // SO_ORIGINAL_DST — специальный socket option Linux
    // возвращает оригинальный dst перед iptables REDIRECT
    raw, _ := conn.(*net.TCPConn).SyscallConn()
    raw.Control(func(fd uintptr) {
        mreq, _ = unix.GetsockoptIPv6Mreq(int(fd), unix.SOL_IP, SO_ORIGINAL_DST)
    })
    return mreq, nil
}
```

### 4.3 Netfilter Queue (Linux, ядро)

Самый эффективный метод — перехват на уровне ядра:

```bash
iptables -A OUTPUT -p tcp --dport 443 -j NFQUEUE --queue-num 0
```

FreeNet получает пакеты через `go-nfqueue`, модифицирует их, возвращает обратно в ядро. Работает для ВСЕХ приложений без настройки SOCKS5.

### 4.4 Android VpnService (Android)

VpnService создаёт виртуальный TUN-интерфейс. Все приложения отправляют трафик через него:

```kotlin
val builder = Builder()
builder.addAddress("10.89.0.1", 24)
builder.addRoute("0.0.0.0", 0)  // весь трафик
builder.setSession("FreeNet")
val tunFd = builder.establish()
```

Go код (через gomobile) читает raw IP пакеты из TUN fd, разбирает TCP/IP заголовки, проксирует через SOCKS5 с bypass.

---

## 5. SOCKS5 прокси

**Файл:** `internal/proxy/socks5.go`

### Протокол (RFC 1928)

```
Клиент → Сервер: [VER=5][NMETHODS=1][METHOD=0x00 (no auth)]
Сервер → Клиент: [VER=5][METHOD=0x00]

Клиент → Сервер: [VER=5][CMD=1][RSV=0][ATYP][DST.ADDR][DST.PORT]
Сервер → Клиент: [VER=5][REP=0][RSV=0][ATYP][BND.ADDR][BND.PORT]

Далее: двунаправленный прокси данных
```

### Жизненный цикл соединения

```
1. Accept TCP от клиента
2. SOCKS5 handshake (auth negotiation)
3. Разбор CONNECT запроса → target host:port
4. Проверить hostlist (нужен ли bypass?)
5. Dial к target
6. Применить bypass стратегию к первому write()
7. io.Copy в обоих направлениях
8. Статистика: bytes_in, bytes_out, bypassed/passthrough
```

---

## 6. Веб-интерфейс

**Файл:** `internal/web/ui.go`

### Endpoints

| Endpoint | Метод | Описание |
|----------|-------|----------|
| `/` | GET | Главная страница с кнопкой вкл/выкл |
| `/downloads` | GET | Страница скачивания для всех платформ |
| `/api/status` | GET | JSON: статус, стратегия, статистика |
| `/api/toggle` | POST | Вкл/Выкл bypass |
| `/api/strategy` | POST | Изменить стратегию |
| `/api/stats` | GET | JSON: bytes_in/out, connections |
| `/ws/logs` | WebSocket | Поток логов в реальном времени |

### WebSocket логи

```javascript
// JavaScript на клиенте
const ws = new WebSocket('ws://localhost:8080/ws/logs');
ws.onmessage = (event) => {
    logContainer.innerHTML += event.data + '\n';
};
```

На сервере:
```go
// ring.go — кольцевой буфер с подписчиками
func (r *Ring) Subscribe() <-chan Entry {
    ch := make(chan Entry, 64)
    r.mu.Lock()
    r.subs = append(r.subs, ch)
    r.mu.Unlock()
    return ch
}
```

---

## 7. Android архитектура

### Слои

```
┌─────────────────────────────────────────────────────┐
│  Kotlin UI Layer (Jetpack Compose)                  │
│  MainActivity.kt + VpnViewModel.kt                  │
└─────────────────────┬───────────────────────────────┘
                      │ ViewModel → Service
┌─────────────────────▼───────────────────────────────┐
│  Android Service Layer                              │
│  FreenetVpnService.kt (VpnService)                  │
│  - Создаёт TUN interface (10.89.0.1/24)             │
│  - Управляет lifecycle VPN                          │
│  - Уведомление в statusbar                          │
└─────────────────────┬───────────────────────────────┘
                      │ tunFd + gomobile API
┌─────────────────────▼───────────────────────────────┐
│  Go Core Layer (gomobile AAR)                       │
│  mobile/engine.go                                   │
│  - SOCKS5 bypass прокси                             │
│  - Все стратегии из internal/bypass/                │
│  - FreenetEngine.StartVPN(tunFd, port, protector)   │
└─────────────────────┬───────────────────────────────┘
                      │ raw IPv4/TCP packets
┌─────────────────────▼───────────────────────────────┐
│  TUN Packet Forwarder                               │
│  mobile/tun.go (Go) / PacketForwarder.kt (Kotlin)   │
│  - Читает IPv4 пакеты из TUN fd                     │
│  - Парсит TCP заголовки (SYN/ACK/FIN/RST/PSH)       │
│  - Проксирует через SOCKS5 bypass                   │
│  - Записывает ответы обратно в TUN                  │
└─────────────────────────────────────────────────────┘
```

### gomobile binding

Функции Go, экспортируемые в Java/Kotlin:

```kotlin
// Kotlin (после сборки AAR)
val engine = Mobile.newFreenetEngine()
engine.start(1080)                    // Int — SOCKS5 порт
engine.setStrategy("auto")           // String
engine.setBypassEnabled(true)        // Boolean
engine.isRunning()                   // Boolean
engine.getVersion()                  // String
engine.getStats()                    // String (JSON)
engine.getRecentLogs(50)             // String
engine.startVPN(tunFd, 1080, prot)   // Long, Int, SocketProtector → error

// Остановка
engine.stop()
```

### VpnService socket protection

Чтобы трафик прокси (SOCKS5 соединения к реальным серверам) не попал снова в TUN
(что привело бы к бесконечному циклу):

```kotlin
class GoSocketProtector(private val svc: VpnService) : Mobile.SocketProtector {
    override fun protect(fd: Long): Boolean = svc.protect(fd.toInt())
}
```

На Go стороне каждый исходящий сокет помечается через `Protect(fd)` перед `connect()`.

---

## 8. Конфигурация

### Файл конфигурации (`config.yaml`)

```yaml
proxy:
  listen_addr: "127.0.0.1:1080"       # SOCKS5
  transparent_addr: "0.0.0.0:1081"   # Прозрачный прокси
  web_addr: "0.0.0.0:8080"           # Веб UI

bypass:
  strategy: "auto"    # auto | split | tlsrec | fake | combined | quic | none
  enabled: true
  split_pos: 2        # позиция split до SNI (байты от начала SNI)
  fake_ttl: 4         # TTL для fake пакетов
  md5_fake: false     # использовать MD5 checksum вместо TTL для fake

nfqueue:
  enabled: false      # включить netfilter queue (требует root)
  queue_num: 0

hostlist:
  enabled: false
  path: "domains.lst"
  auto_update: true
  update_url: "https://antifilter.download/list/domains.lst"
  update_interval: "24h"
```

### CLI флаги

```
./freenet [flags]

  -config string      Путь к config.yaml (по умолчанию: config.yaml)
  -web string         Адрес веб UI (по умолчанию: :8080)
  -socks string       Адрес SOCKS5 (по умолчанию: :1080)
  -strategy string    Стратегия bypass (по умолчанию: auto)
  -nfqueue            Включить nfqueue режим
  -install            Установить как Windows Service
  -uninstall          Удалить Windows Service
```

### Переменные окружения (Docker)

```bash
FREENET_STRATEGY=auto          # стратегия bypass
FREENET_WEB_ADDR=:8080         # порт веб UI
FREENET_SOCKS_ADDR=:1080       # порт SOCKS5
FREENET_TRANSPARENT=true       # включить прозрачный прокси
FREENET_NFQUEUE=false          # включить nfqueue
```

---

## 9. Сборка и деплой

### Требования

| Инструмент | Версия | Назначение |
|------------|--------|-----------|
| Go | 1.26+ | Компиляция |
| Docker | 24+ | Контейнер |
| Android NDK | 26.3+ | gomobile |
| Android SDK | API 26+ | Android APK |
| gomobile | latest | Go→Android binding |

### Docker

```bash
# Собрать и запустить
cd go-freenet
docker compose up -d

# Конфигурация через env
FREENET_STRATEGY=combined docker compose up -d

# Логи
docker compose logs -f freenet
```

**Dockerfile** использует multi-stage build:
- Stage 1: `golang:1.26` — компиляция бинарника
- Stage 2: `debian:bookworm-slim` — финальный образ (~15 MB)

### Linux (systemd)

```bash
sudo bash scripts/install.sh
# → копирует бинарник в /usr/local/bin/freenet
# → создаёт /etc/freenet/config.yaml
# → устанавливает systemd unit
# → запускает freenet.service

sudo systemctl status freenet
journalctl -u freenet -f
```

### Windows (служба)

```powershell
# Установить как Windows Service
.\freenet-windows-amd64.exe -install

# Или через PowerShell one-liner (скачивает и устанавливает)
irm https://github.com/mintfary-oss/zapret2-may/releases/latest/download/install-windows.ps1 | iex

# Управление
Start-Service FreeNet
Stop-Service FreeNet
```

### Android

```bash
# Требования
export ANDROID_NDK_HOME=$HOME/Android/Sdk/ndk/26.3.11579264

# Полная сборка (AAR + APK)
cd go-freenet
bash scripts/build-release-apk.sh
# → dist/freenet-android.apk

# Только AAR (для разработчиков)
bash scripts/build-android.sh
# → android/app/libs/mobile.aar
```

---

## 10. CI/CD Pipeline

**Файл:** `.github/workflows/freenet.yml`

### Триггеры

```yaml
on:
  push:
    branches: ["master", "main", "neo/**"]
    tags: ["freenet-v*.*.*"]     # → создаёт GitHub Release
    paths: ["go-freenet/**"]
  pull_request:
    paths: ["go-freenet/**"]
  workflow_dispatch: {}           # ручной запуск
```

### Jobs

```
Lint (go vet + gofmt)
    │
    ├──▶ Build (matrix: linux/amd64, arm64, armv7, windows/amd64)
    │        │
    │        └──▶ Package Linux installer
    │
    └──▶ Android APK (gomobile + Gradle)
              │
              └──▶ (только при tag) Create GitHub Release
```

### Создание нового релиза

```bash
# 1. Убедиться что master готов
git log master --oneline -5

# 2. Создать тег
git tag freenet-v1.1.0
git push origin freenet-v1.1.0

# GitHub Actions автоматически:
# - Запускает все 5 jobs
# - Собирает 4 платформы + Android APK
# - Создаёт GitHub Release с 9 артефактами
# - Публикует ссылки для скачивания
```

---

## 11. Безопасность

### Сетевая безопасность

- SOCKS5 по умолчанию слушает только `127.0.0.1` — недоступен с других устройств
- Веб UI по умолчанию слушает `0.0.0.0:8080` — **смените на** `127.0.0.1:8080` если не нужен доступ из локальной сети

### Android безопасность

- VpnService API — официальный Android механизм, не требует root
- Приложение видит все пакеты через TUN интерфейс
- Socket protection предотвращает routing loop
- Foreground Service + уведомление — пользователь всегда знает что VPN активен

### Fake packets и привилегии

- `fake` стратегия требует `CAP_NET_RAW`
- В Docker: `cap_add: [NET_ADMIN, NET_RAW]`
- В Linux: `sudo setcap cap_net_raw+ep /usr/local/bin/freenet`
- Без привилегий стратегия fallback на `split`

### Что FreeNet НЕ делает

- Не шифрует трафик (это не VPN)
- Не скрывает IP-адрес пользователя
- Не защищает от трекинга или слежки
- Не обходит авторизацию на сайтах

FreeNet только помогает установить соединение с заблокированным ресурсом, обходя DPI блокировку по SNI.
