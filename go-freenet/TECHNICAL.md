# Техническая документация — FreeNet Go

## Содержание

1. [Обзор архитектуры](#1-обзор-архитектуры)
2. [Как работает обход DPI](#2-как-работает-обход-dpi)
3. [Стратегии обхода](#3-стратегии-обхода)
4. [DNS-over-HTTPS защита](#4-dns-over-https-защита)
5. [Перехват трафика](#5-перехват-трафика)
6. [SOCKS5 прокси](#6-socks5-прокси)
7. [Веб-интерфейс](#7-веб-интерфейс)
8. [Android архитектура](#8-android-архитектура) (TUN, UDP relay, split tunnel, widget)
9. [Windows интеграция](#9-windows-интеграция) (трей, системный прокси, WinDivert, служба)
10. [Конфигурация](#10-конфигурация)
11. [Сборка и деплой](#11-сборка-и-деплой)
12. [CI/CD pipeline](#12-cicd-pipeline)
13. [Безопасность](#13-безопасность)

---

## 1. Обзор архитектуры

FreeNet Go — кросс-платформенный инструмент обхода блокировок, написанный на Go.
Сочетает обход DPI (манипуляция TCP/TLS пакетами) и защиту DNS (DNS-over-HTTPS).

```
Браузер / Приложение
        │
        │ SOCKS5 (127.0.0.1:1080)
        ▼
┌───────────────────────────────────┐
│         SOCKS5 прокси             │
│   internal/proxy/socks5.go        │
└──────────────┬────────────────────┘
               │ TCP соединение к target
               ▼
┌───────────────────────────────────┐
│       DPI Bypass Engine           │
│   internal/bypass/engine.go       │
│                                   │
│  split / tlsrec / fake / combined │
│  disorder / quic / auto           │
└──────────────┬────────────────────┘
               │ модифицированные TCP сегменты
               ▼
           Интернет (мимо ТСПУ)

Параллельно работает DNS защита:
        │
        │ DNS запрос (UDP :53)
        ▼
┌───────────────────────────────────┐
│     Локальный DoH резолвер        │
│   internal/dns/resolver.go        │
│   слушает 127.0.0.1:5300          │
└──────────────┬────────────────────┘
               │ HTTPS POST application/dns-message
               ▼
    1.1.1.1 / 8.8.8.8 / 9.9.9.9
    (Cloudflare / Google / Quad9)
```

Для автоматического перехвата всего трафика (Linux, без настройки SOCKS5):

```
Любое приложение
        │ обычный TCP (порт 443)
        ▼
  iptables / nfqueue (Linux)
        │
        ▼
┌───────────────────────────────────┐
│   Прозрачный прокси / nfqueue     │
│  transparent.go / nfqueue.go      │
└──────────────┬────────────────────┘
               ▼
         DPI Bypass Engine
```

Для Android (без root):

```
Любое приложение
        │ все пакеты через TUN
        ▼
┌───────────────────────────────────┐
│   Android VpnService TUN          │
│   mobile/tun.go                   │
│                                   │
│   TCP → SOCKS5 bypass прокси      │
│   UDP:53 → DoH (inline, без порта)│
└───────────────────────────────────┘
```

---

## 2. Как работает обход DPI

### Что такое DPI (Deep Packet Inspection)

Российская система ТСПУ использует DPI-оборудование для:

1. **Анализа TLS ClientHello** — в незашифрованном заголовке TLS виден SNI (Server Name Indication) — домен, к которому подключается клиент
2. **Блокировки по SNI** — если SNI в списке блокировки → соединение сбрасывается TCP RST
3. **DNS-подмены** — провайдер возвращает неправильный IP для заблокированных доменов
4. **QUIC fingerprinting** — аналогично для HTTP/3

### Двойная защита FreeNet

**Уровень 1 — DPI bypass:** Манипуляция TCP/TLS пакетами так, чтобы DPI не мог собрать полный TLS ClientHello и определить SNI.

**Уровень 2 — DNS защита:** Все DNS запросы идут через HTTPS к зарубежным резолверам — провайдер не видит и не может подменить DNS-ответы.

**Ключевой принцип DPI bypass:** Отправляем данные "не по порядку" или дополненные "мусором" с точки зрения DPI, но корректные для TCP-стека на обоих концах соединения.

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
func relaySplit(client, remote net.Conn, splitPos int) {
    buf := make([]byte, 4096)
    n, _ := client.Read(buf)
    data := buf[:n]
    pos := findSNIOffset(data)
    if pos > 0 && pos < len(data) {
        remote.Write(data[:pos])
        remote.Write(data[pos:])
    } else {
        remote.Write(data)
    }
    // далее io.Copy в обоих направлениях
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
Работает против DPI-систем с буферизацией на уровне TLS.

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

DPI-система обрабатывает весь трафик, включая умершие пакеты. Видит fake ClientHello
(с мусорным SNI или invalid checksum), считает соединение завершённым.

**MD5 Fake вариант** (когда TTL не работает): отправляем пакет с неправильной TCP checksum.
Промежуточные DPI боксы часто принимают такие пакеты, конечный хост — нет.

**Требования:** `CAP_NET_RAW` (root или `setcap cap_net_raw+ep freenet`).

**Реализация (Linux):**
```go
//go:build linux
func sendFakePacket(dstIP net.IP, dstPort uint16, fakeTTL int) error {
    fd, _ := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_RAW)
    defer syscall.Close(fd)
    pkt := buildIPPacket(dstIP, dstPort, byte(fakeTTL), buildFakeClientHello())
    addr := &syscall.SockaddrInet4{Port: int(dstPort)}
    copy(addr.Addr[:], dstIP.To4())
    return syscall.Sendto(fd, pkt, 0, addr)
}
```

---

### 3.4 QUIC Bypass

**Файл:** `internal/bypass/quic.go`

**Принцип:** QUIC (HTTP/3) использует UDP порт 443. Первый датаграмм — QUIC Initial — содержит SNI в открытом виде (до установки шифрования).

Фрагментируем первый датаграмм QUIC Initial на несколько UDP пакетов.
DPI не может восстановить полный QUIC Initial для анализа SNI.

---

### 3.5 Auto-Detect

**Файл:** `internal/bypass/autodetect.go`

Алгоритм:
1. Пробуем все стратегии по очереди: `combined` → `fake` → `tlsrec` → `split` → `disorder` → `none`
2. Отправляем минимальный TLS ClientHello к probe-цели (по умолчанию `1.1.1.1:443`)
3. Проверяем получение ответа (TLS ServerHello или любые данные)
4. Первая успешная стратегия кэшируется как `winner`
5. Все последующие подключения используют эту стратегию

```go
func (d *AutoDetector) Run(target string, strategies []string, splitPos int) []ProbeResult {
    for _, s := range strategies {
        r := probeStrategy(target, s, splitPos)
        if r.OK && d.winner == "" {
            d.winner = s  // кэшируем первую успешную
        }
    }
}
```

---

### 3.6 Hostlist

**Файл:** `internal/bypass/hostlist.go`

Bypass применяется только к доменам из списка заблокированных. Для остальных сайтов — прямое соединение без модификации (меньше latency, меньше нагрузки).

Источники списков:
- `antifilter.download/list/domains.lst` — 800K+ доменов из реестра РКН
- Локальный файл `domains.lst`

Загрузка списков использует DoH-aware HTTP клиент (начиная с v1.3.0) — сам URL резолвится через DoH.

---

## 4. DNS-over-HTTPS защита

### 4.1 Проблема

Российские провайдеры применяют два метода блокировки одновременно:
1. **DPI по SNI** — сбрасывают TCP соединение если видят заблокированный SNI
2. **DNS подмена** — возвращают неправильный IP (127.0.0.1 или страницу блокировки)

Стандартный DNS (UDP порт 53) — открытый протокол без шифрования. Провайдер может:
- Подменить ответ (вернуть неправильный IP)
- Заблокировать запрос полностью
- Логировать все запросы

### 4.2 DNS-over-HTTPS (RFC 8484)

DoH оборачивает DNS запросы в обычный HTTPS запрос:

```
Клиент → POST https://1.1.1.1/dns-query
Content-Type: application/dns-message
[wire-format DNS query в теле]

Ответ ← 200 OK
Content-Type: application/dns-message
[wire-format DNS ответ в теле]
```

Провайдер видит только HTTPS соединение к `1.1.1.1`. DNS запросы зашифрованы и неотличимы от обычного HTTPS трафика.

### 4.3 Реализация: DoH Client

**Файл:** `internal/dns/doh.go`

```go
type Client struct {
    servers    []string        // ["https://1.1.1.1/dns-query", ...]
    httpClient *http.Client    // стандартный HTTP клиент
}

// Exchange — отправляет wire-format DNS запрос, возвращает wire-format ответ.
// Пробует серверы по очереди, возвращает первый успех.
func (c *Client) Exchange(ctx context.Context, query []byte) ([]byte, error) {
    for _, srv := range c.servers {
        resp, err := c.doQuery(ctx, srv, query)
        if err == nil {
            return resp, nil
        }
    }
    return nil, fmt.Errorf("all DoH servers failed")
}

func (c *Client) doQuery(ctx context.Context, server string, query []byte) ([]byte, error) {
    req, _ := http.NewRequestWithContext(ctx, "POST", server, bytes.NewReader(query))
    req.Header.Set("Content-Type", "application/dns-message")
    req.Header.Set("Accept", "application/dns-message")
    resp, _ := c.httpClient.Do(req)
    return io.ReadAll(resp.Body)
}
```

DNS wire format (RFC 1035) строится через `golang.org/x/net/dns/dnsmessage`:

```go
func buildQuery(name string, qtype dnsmessage.Type) ([]byte, error) {
    fqdn, _ := dnsmessage.NewName(name + ".")
    msg := dnsmessage.Message{
        Header: dnsmessage.Header{ID: 1, RecursionDesired: true},
        Questions: []dnsmessage.Question{
            {Name: fqdn, Type: qtype, Class: dnsmessage.ClassINET},
        },
    }
    return msg.Pack()
}
```

### 4.4 Реализация: Локальный UDP резолвер

**Файл:** `internal/dns/resolver.go`

Запускается на `127.0.0.1:5300`. Принимает обычные UDP DNS запросы, форвардирует через DoH, возвращает ответы.

```go
type Resolver struct {
    client     *Client
    listenAddr string
    conn       *net.UDPConn
    queries    atomic.Int64   // счётчик для веб UI
    errors     atomic.Int64
}

func (r *Resolver) forward(ctx context.Context, src *net.UDPAddr, query []byte) {
    r.queries.Add(1)
    resp, err := r.client.Exchange(ctx, query)
    if err != nil {
        r.errors.Add(1)
        return
    }
    r.conn.WriteToUDP(resp, src)  // отправляем ответ обратно клиенту
}
```

### 4.5 Реализация: DoH-aware HTTP клиент

**Файл:** `internal/dns/doh.go` — функция `NewDoHHTTPClient`

Для защиты самих HTTPS запросов (загрузка hostlist, авто-обновления) используем
HTTP клиент, который резолвит имена через локальный DoH резолвер:

```go
func NewDoHHTTPClient(resolverAddr string) *http.Client {
    // net.Resolver с кастомным Dial — все DNS запросы идут к нашему резолверу
    r := &net.Resolver{
        PreferGo: true,
        Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
            return (&net.Dialer{}).DialContext(ctx, "udp", resolverAddr)
        },
    }
    d := &net.Dialer{Resolver: r}
    return &http.Client{
        Timeout: 30 * time.Second,
        Transport: &http.Transport{DialContext: d.DialContext},
    }
}
```

### 4.6 Реализация: Android TUN DNS перехват

**Файл:** `mobile/tun.go`

На Android нет возможности запустить отдельный UDP порт для DoH резолвера (без root).
Поэтому TUN forwarder перехватывает UDP пакеты к порту 53 прямо внутри TUN loop:

```go
func (fw *tunForwarder) handlePacket(pkt []byte) {
    proto := pkt[9]
    
    // UDP DNS перехват — только если DoH клиент задан
    if proto == 17 && fw.dohClient != nil {
        udp := pkt[ihl:]
        dstPort := binary.BigEndian.Uint16(udp[2:4])
        if dstPort == 53 {
            payload := udp[8:udpLen]
            go fw.handleDNSQuery(srcIP, dstIP, srcPort, dstPort, payload)
            return
        }
    }
    
    // TCP — обычный bypass через SOCKS5
    if proto == 6 { ... }
}

func (fw *tunForwarder) handleDNSQuery(...) {
    resp, _ := fw.dohClient.Exchange(ctx, query)
    // Строим UDP ответный пакет и инжектируем обратно в TUN
    pkt := buildUDPPacket(dstIP[:], srcIP[:], dstPort, srcPort, resp)
    fw.tun.Write(pkt)
}
```

UDP response packet строится вручную (IPv4 + UDP заголовки с корректными checksum):

```
[IPv4 Header: 20 bytes]
  version=4, IHL=5, TTL=64, protocol=17
  src=DNS_SERVER_IP, dst=DEVICE_IP
  IP checksum (ones complement)

[UDP Header: 8 bytes]
  srcPort=53, dstPort=ORIGINAL_SRC_PORT
  length, UDP checksum (с pseudo-header)

[DNS Response payload]
```

### 4.7 Интеграция в прокси-сервер

**Файл:** `internal/proxy/server.go`

При старте сервера (если `dns.enabled: true` в конфиге):

```go
func (s *Server) Start() error {
    if s.cfg.DNS.Enabled {
        dohClient := dns.NewClient(s.cfg.DNS.Servers)
        res := dns.NewResolver(s.cfg.DNS.ListenAddr, dohClient)
        if err := res.Start(context.Background()); err == nil {
            s.dnsRes = res
            // Hostlist загружается через DoH-защищённый HTTP клиент
            s.engine.SetHTTPClient(dns.NewDoHHTTPClient(s.cfg.DNS.ListenAddr))
        }
    }
    // ... запуск SOCKS5 и т.д.
}
```

### 4.8 Статус в веб-интерфейсе

API `/api/status` возвращает:
```json
{
  "enabled": true,
  "strategy": "auto",
  "dns_enabled": true,
  "dns_queries": 1234,
  "dns_errors": 0
}
```

Веб UI отображает:
- `🔒 DNS-over-HTTPS активен · запросов: 1234` (зелёный)
- `⚠ DNS-over-HTTPS выключен (DNS может быть подменён)` (жёлтый)

---

## 5. Перехват трафика

### 5.1 SOCKS5 (ручная настройка)

Пользователь настраивает браузер или приложение использовать SOCKS5 `127.0.0.1:1080`.

- **Плюсы:** Работает везде, не нужны права root.
- **Минусы:** Нужно настраивать каждое приложение отдельно.
- **Windows:** Автоматически — системный прокси устанавливается при включении bypass (v1.2.0+).

### 5.2 Прозрачный прокси (Linux)

Все TCP соединения автоматически перехватываются через iptables:

```bash
# Перехватываем весь исходящий TCP трафик на порт 443
iptables -t nat -A OUTPUT -p tcp --dport 443 -j REDIRECT --to-ports 1081
```

FreeNet слушает на порту 1081 и использует `SO_ORIGINAL_DST` для получения оригинального
адреса назначения (до редиректа).

### 5.3 Netfilter Queue (Linux, ядро)

**Файл:** `internal/proxy/nfqueue.go`

Самый эффективный метод — перехват на уровне ядра:

```bash
iptables -A OUTPUT -p tcp --dport 443 -j NFQUEUE --queue-num 200
```

FreeNet получает пакеты через `go-nfqueue`, модифицирует их, возвращает обратно в ядро.
Работает для ВСЕХ приложений без настройки SOCKS5. Требует `CAP_NET_ADMIN`.

### 5.4 Android VpnService

VpnService создаёт виртуальный TUN-интерфейс. Все приложения отправляют трафик через него:

```kotlin
val builder = Builder()
builder.addAddress("10.89.0.1", 24)
builder.addRoute("0.0.0.0", 0)      // весь трафик через VPN
builder.addDnsServer("8.8.8.8")     // DNS тоже через VPN (будет перехвачен DoH)
builder.setSession("FreeNet")
val tunFd = builder.establish()     // fd передаётся в Go через gomobile
```

Go код читает raw IPv4 пакеты из TUN fd:
- **TCP пакеты** → TCP state machine → SOCKS5 bypass прокси
- **UDP пакеты к порту 53** → DoH клиент → ответ инжектируется обратно в TUN

---

## 6. SOCKS5 прокси

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
6. Применить bypass стратегию к первому write() (TLS ClientHello)
7. io.Copy в обоих направлениях
8. Статистика: bytes_in, bytes_out, bypassed/passthrough
```

---

## 7. Веб-интерфейс

**Файл:** `internal/web/ui.go`

### Endpoints

| Endpoint | Метод | Описание |
|----------|-------|----------|
| `/` | GET | Главная страница с кнопкой вкл/выкл |
| `/download` | GET | Redirect → `/?tab=download` |
| `/api/status` | GET | JSON: статус, стратегия, DoH, hostlist |
| `/api/toggle` | POST | Вкл/Выкл bypass |
| `/api/strategy` | POST | Изменить стратегию |
| `/api/stats` | GET | JSON: bytes_in/out, connections |
| `/api/autodetect` | POST | Запустить авто-определение стратегии |
| `/ws/logs` | WebSocket | Поток логов в реальном времени |

### Ответ `/api/status`

```json
{
  "enabled": true,
  "strategy": "auto",
  "listen_addr": "127.0.0.1:1080",
  "hostlist_size": 823041,
  "dns_enabled": true,
  "dns_queries": 5678,
  "dns_errors": 0
}
```

### WebSocket логи

Сервер пишет все `log.Printf()` в кольцевой буфер (`internal/logs/ring.go`).
При подключении WebSocket клиент получает последние 100 строк, затем подписывается
на новые через Go channel.

---

## 8. Android архитектура

### Слои (v1.6.0)

```
┌─────────────────────────────────────────────────────────────┐
│  Kotlin UI Layer (Jetpack Compose)                          │
│  MainActivity.kt + VpnViewModel.kt                          │
│  - BigToggleButton, StrategyPicker, StatsCard, LogCard      │
│  - SplitTunnelCard (per-app selector + поиск)               │
│  + FreeNetWidget.kt (AppWidget 2×1 на рабочем столе)       │
└─────────────────────┬───────────────────────────────────────┘
                      │ ViewModel → Service
┌─────────────────────▼───────────────────────────────────────┐
│  Android Service Layer                                      │
│  FreenetVpnService.kt (VpnService)                          │
│  - TUN interface: только IPv4 (без ::/0)                    │
│  - Split tunnel: addAllowedApplication/addDisallowedApp     │
│  - SplitTunnelConfig.kt (SharedPreferences persist)         │
│  - Уведомление + ACTION_START/STOP broadcasts               │
└─────────────────────┬───────────────────────────────────────┘
                      │ tunFd + gomobile API
┌─────────────────────▼───────────────────────────────────────┐
│  Go Core Layer (gomobile AAR)                               │
│  mobile/engine.go                                           │
│  - SOCKS5 bypass прокси (все стратегии)                     │
│  - FreenetEngine.StartVPN(tunFd, port, protector)           │
└─────────────────────┬───────────────────────────────────────┘
                      │ raw IPv4 packets
┌─────────────────────▼───────────────────────────────────────┐
│  TUN Packet Forwarder                                       │
│  mobile/tun.go — диспетчер протоколов                      │
│  ├── TCP (proto=6)  → SOCKS5 bypass engine                  │
│  ├── UDP:53         → DoH inline resolution                 │
│  └── UDP (other)    → mobile/tun_udp.go (UDP NAT relay)     │
└─────────────────────┬───────────────────────────────────────┘
                      │ protected UDP sockets
┌─────────────────────▼───────────────────────────────────────┐
│  UDP NAT Relay                                              │
│  mobile/tun_udp.go                                          │
│  - Таблица udpSession (srcIP:port → dstIP:port → socket)    │
│  - Protect socket: исключает из VPN routing                 │
│  - Sweeper: закрывает idle сессии (>30 сек)                 │
│  Покрывает: Discord (UDP), Steam, игры, QUIC               │
└─────────────────────────────────────────────────────────────┘
```

### gomobile binding

Функции Go, экспортируемые в Java/Kotlin (пакет `com.freenet.bypass` — из `-javapkg`):

```kotlin
// Фабричный метод — класс Mobile соответствует Go-пакету "mobile"
val engine = com.freenet.bypass.Mobile.newFreenetEngine()
engine.start(1080)                    // запуск SOCKS5 прокси
engine.setStrategy("auto")           // стратегия bypass
engine.setBypassEnabled(true)        // вкл/выкл bypass
engine.isRunning()                   // статус
engine.getVersion()                  // "1.8.2"
engine.getStats()                    // JSON статистика
engine.getRecentLogs(50)             // последние 50 строк лога
engine.startVPN(tunFd, 1080, prot)   // блокирующий вызов: запуск TUN loop
engine.stop()
```

### VpnService socket protection

Чтобы трафик прокси не попал снова в TUN (routing loop).
`FreenetVpnService.kt` создаёт `SocketProtector` через reflection (не прямой импорт,
так как AAR может отсутствовать при сборке без gomobile):

```kotlin
// com.freenet.bypass.SocketProtector — интерфейс из gomobile AAR
val protectorCls = Class.forName("com.freenet.bypass.SocketProtector")
val protector = java.lang.reflect.Proxy.newProxyInstance(
    protectorCls.classLoader,
    arrayOf(protectorCls)
) { _, _, args ->
    val fd = (args[0] as Long).toInt()
    protect(fd)  // VpnService.protect() — исключает сокет из VPN routing
}
```

Каждый исходящий сокет (к реальному серверу через SOCKS5 или UDP relay) помечается
через `protect(fd)` до `connect()`. ОС исключает эти сокеты из VPN routing.

### UDP NAT Relay (`mobile/tun_udp.go`)

Для каждого UDP flow (srcIP:srcPort → dstIP:dstPort) создаётся локальный `UDPConn`:

```
Приложение → TUN (UDP) → handleUDPRelay() → protected UDPConn → Internet
Internet → protected UDPConn → relayUDPResponses() → inject UDP в TUN → Приложение
```

**Жизненный цикл сессии:**
1. `handleUDPRelay()` — LoadOrStore в `udpConns sync.Map`
2. Новая сессия: `net.ListenPacket("udp4", "0.0.0.0:0")` + `protect(fd)`
3. `relayUDPResponses()` горутина: ReadFrom → buildUDPPacket → writePkt
4. ReadDeadline = 30 сек; при timeout — сессия закрывается
5. `sweepIdleUDPSessions()` — тикер каждые 15 сек, чистит просроченные

**Что работает после v1.6.0:**
- Discord (UDP голос/видео)
- Steam (matchmaking, game traffic)
- Все QUIC/HTTP3 соединения на нестандартных портах
- Видеозвонки (Telegram VoIP, WhatsApp, Zoom)

### Per-app split tunnel (`SplitTunnelConfig.kt`)

```kotlin
// Три режима
SplitTunnelConfig.MODE_DISABLED   // все приложения через VPN
SplitTunnelConfig.MODE_ALLOWLIST  // только выбранные через VPN
SplitTunnelConfig.MODE_BLOCKLIST  // все, кроме выбранных

// Применение в VpnService.Builder
when (config.mode) {
    MODE_ALLOWLIST -> apps.forEach { builder.addAllowedApplication(it) }
    MODE_BLOCKLIST -> apps.forEach { builder.addDisallowedApplication(it) }
}
```

Конфиг сериализуется в SharedPreferences (`freenet_split_tunnel`):
- `mode` → строка
- `apps_json` → JSON массив package name

### Home screen widget (`FreeNetWidget.kt`)

`AppWidgetProvider` с layout `res/layout/widget_toggle.xml` (2×1 ячейки):

```
┌──────────────────────┐
│  FreeNet ВКЛ   (🟢) │  ← когда VPN активен
└──────────────────────┘
┌──────────────────────┐
│  FreeNet ВЫКЛ  (🔴) │  ← когда VPN остановлен
└──────────────────────┘
```

Обновление виджета:
- `FreenetVpnService` рассылает `ACTION_START` / `ACTION_STOP`
- `FreeNetWidget.onReceive()` вызывает `update(ctx)` → `updateWidget()`
- `RemoteViews.setInt(id, "setBackgroundColor", color)` меняет цвет

### gomobile reflection — важные правила (v1.8.1)

gomobile генерирует Java-классы по особым правилам. При использовании рефлексии
в Kotlin необходимо учитывать:

**1. Имена классов**

gomobile экспортирует Go-интерфейсы как **top-level Java классы**, не как вложенные.
```kotlin
// ❌ НЕВЕРНО — вложенный класс не существует:
Class.forName("mobile.Mobile\$SocketProtector")

// ✅ ВЕРНО — top-level Java класс:
Class.forName("mobile.SocketProtector")
```

**2. Примитивные типы vs boxed**

gomobile генерирует методы с Java-примитивами (`long`, `int`), не с boxed-типами
(`java.lang.Long`, `java.lang.Integer`). `getMethod()` строго различает их.
```kotlin
// ❌ НЕВЕРНО — boxed тип: java.lang.Long
.getMethod("startVPN", Long::class.java, Int::class.java, ...)
// → NoSuchMethodException при runtime

// ✅ ВЕРНО — primitive тип: long
.getMethod("startVPN", java.lang.Long.TYPE, java.lang.Integer.TYPE, ...)
```

**Следствие:** Оба бага вызывали тихий `ClassNotFoundException` / `NoSuchMethodException`,
которые перехватывались catch-блоком → `tryStartGoVPN()` возвращал `false` →
fallback на pure-Kotlin `PacketForwarder` (пытался подключиться к `127.0.0.1:1080`,
где SOCKS5 прокси не был запущен) → VPN «подключался», но весь трафик падал.

Исправлено в **v1.8.1** (`FreenetVpnService.kt`).

---

## 9. Windows интеграция

### 9.1 Системный трей

**Файл:** `cmd/freenet/tray_windows.go`

При запуске на Windows вместо консоли запускается системный трей через `github.com/getlantern/systray`.

Меню трея:
```
[F] FreeNet (иконка)
─────────────────────
● Bypass включён        ← статус
↗ Системный прокси: установлен
─────────────────────
  Выключить bypass      ← действие
─────────────────────
  Стратегия: auto
    [✓] Auto
    [ ] Split
    [ ] TLS Record
    [ ] Combined
    [ ] Fake packets
    [ ] QUIC bypass
    [ ] Нет bypass
─────────────────────
  Открыть веб-интерфейс
─────────────────────
  Выйти из FreeNet
```

### 9.2 Автоматический системный прокси

**Файл:** `internal/sysproxy/sysproxy_windows.go`

При включении bypass FreeNet записывает настройки в реестр Windows:

```
HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Internet Settings
  ProxyEnable  = 1 (DWORD)
  ProxyServer  = "socks=127.0.0.1:1080"
```

При выключении — восстанавливает оригинальные значения.

После изменения реестра отправляется системное сообщение `WM_SETTINGCHANGE` через
`SendMessageTimeout(HWND_BROADCAST, ...)` — Chrome, Edge, Firefox подхватывают изменения
без перезапуска.

### 9.3 WinDivert — перехват на уровне ядра

**Файлы:** `internal/windivert/windivert_windows.go`, `cmd/freenet/windivert_windows.go`

WinDivert перехватывает все исходящие TCP-пакеты на порт 443 до того, как они
покидают сетевой стек. Это позволяет применять DPI bypass для **всех приложений**
без настройки SOCKS5 прокси.

**Загрузка DLL без CGO:**
```go
// Загрузка WinDivert.dll через стандартный syscall.NewLazyDLL
// Нет CGO — кросс-компиляция с Linux работает без изменений.
var (
    winDivertDLL        = syscall.NewLazyDLL("WinDivert.dll")
    procWinDivertOpen   = winDivertDLL.NewProc("WinDivertOpen")
    procWinDivertRecv   = winDivertDLL.NewProc("WinDivertRecv")
    procWinDivertSend   = winDivertDLL.NewProc("WinDivertSend")
    procWinDivertCalcCS = winDivertDLL.NewProc("WinDivertHelperCalcChecksums")
)
```

**Фильтр перехвата:**
```
outbound and !loopback and tcp.DstPort == 443
```
Перехватываются только исходящие не-loopback TCP-пакеты на 443.
SOCKS5 соединения через 127.0.0.1 исключаются автоматически (loopback).

**Алгоритм обработки пакета:**
```
Получить пакет (WinDivertRecv)
    ↓
IPv4 TCP? Нет → реинъекция без изменений
    ↓
Есть payload? Нет (SYN/ACK) → реинъекция
    ↓
TLS ClientHello? (0x16 0x03, HandshakeType=0x01)
Нет → реинъекция
    ↓
HasECH? Да → реинъекция (SNI уже зашифрован, bypass не нужен)
    ↓
Применить стратегию:
  split/combined/auto → split на позиции SNI (два пакета)
  tlsrec             → split после TLS record header (+5 байт)
  иное               → реинъекция
    ↓
Пересчитать контрольные суммы (WinDivertHelperCalcChecksums)
    ↓
Реинъекция (WinDivertSend)
```

**Трей-статус:**
```
⚡ WinDivert: активен (все приложения)   ← если DLL загружена и работает
○ WinDivert: остановлен                  ← если bypass выключен
○ WinDivert: dll не найден (SOCKS5 активен)  ← если WinDivert.dll отсутствует
```

**Fallback:** Если `WinDivert.dll` отсутствует, FreeNet работает в SOCKS5+системный
прокси режиме. Это обеспечивает совместимость с системами без привилегий
администратора.

### 9.4 Windows Service

**Файл:** `cmd/freenet/service_windows.go`

```
freenet.exe -install    → регистрирует как Windows Service
freenet.exe -uninstall  → удаляет из служб
```

Служба запускается автоматически при загрузке Windows под аккаунтом `LocalSystem`.

---

## 10. Конфигурация

### Файл конфигурации (`config.yaml`)

```yaml
proxy:
  listen_addr: "127.0.0.1:1080"       # SOCKS5
  transparent_addr: ""                # Прозрачный прокси (пустое = выключен)

bypass:
  strategy: "auto"    # auto | split | tlsrec | fake | combined | quic | none
  split_pos: 2        # позиция split до SNI (байты)
  fake_ttl: 8         # TTL для fake пакетов (умирает до целевого сервера)
  md5_fake: false     # bad checksum вместо TTL

nfqueue:
  enabled: false      # netfilter queue (Linux, требует CAP_NET_ADMIN)
  queue_num: 200

hostlist:
  enabled: false
  path: "domains.lst"
  auto_update: true
  url: "https://antifilter.download/list/domains.lst"

dns:
  enabled: true
  listen_addr: "127.0.0.1:5300"    # локальный DoH резолвер
  servers:                          # DoH серверы (пустое = Cloudflare+Google+Quad9)
    - "https://1.1.1.1/dns-query"
    - "https://8.8.8.8/dns-query"
    - "https://9.9.9.9/dns-query"
```

### CLI флаги

```
./freenet [flags]

  -config string      Путь к config.yaml (по умолчанию: config.yaml)
  -web string         Адрес веб UI (по умолчанию: :8080)
  -socks string       Адрес SOCKS5 (по умолчанию: :1080)
  -strategy string    Стратегия bypass (по умолчанию: auto)
  -install            Установить как Windows Service
  -uninstall          Удалить Windows Service
```

---

## 11. Сборка и деплой

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

# Логи
docker compose logs -f freenet

# Веб UI доступен на http://localhost:8080
# SOCKS5 на 127.0.0.1:1080
# DoH резолвер на 127.0.0.1:5300
```

**Dockerfile** использует multi-stage build:
- Stage 1: `golang:1.26` — компиляция бинарника
- Stage 2: `debian:bookworm-slim` — финальный образ (~15 MB)

Docker контейнер имеет `cap_add: [NET_ADMIN, NET_RAW]` для fake packets и transparent proxy.

### Linux (systemd)

```bash
sudo bash scripts/install.sh
# → копирует бинарник в /usr/local/bin/freenet
# → создаёт /etc/freenet/config.yaml
# → устанавливает systemd unit
# → запускает freenet.service

sudo systemctl status freenet
journalctl -u freenet -f

# Для fake packets (raw socket)
sudo setcap cap_net_raw+ep /usr/local/bin/freenet
```

### Windows (служба)

```powershell
# Установить как Windows Service (запускается автоматически при загрузке)
.\freenet-windows-amd64.exe -install

# Или PowerShell one-liner (скачивает и устанавливает)
irm https://github.com/mintfary-oss/zapret2-may/releases/latest/download/install-windows.ps1 | iex

# Управление
Start-Service FreeNet
Stop-Service FreeNet
Get-Service FreeNet
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

## 12. CI/CD Pipeline

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
                   ├── freenet-android.apk
                   ├── freenet-windows-amd64.exe
                   ├── freenet-linux-{amd64,arm64,armv7}
                   ├── freenet-linux-amd64-installer.tar.gz
                   ├── install.sh
                   ├── install-windows.ps1
                   └── mobile.aar
```

### Создание нового релиза

```bash
# 1. Убедиться что master готов
git log master --oneline -5

# 2. Создать тег
git tag freenet-v1.4.0
git push origin freenet-v1.4.0

# GitHub Actions автоматически:
# - Запускает все jobs (~5-8 минут)
# - Собирает 4 платформы + Android APK
# - Создаёт GitHub Release с 9 артефактами
# - Публикует страницу релиза с ссылками
```

---

## 13. Безопасность

### Сетевая безопасность

- SOCKS5 по умолчанию слушает только `127.0.0.1` — недоступен с других устройств
- DoH резолвер слушает только `127.0.0.1:5300` — только локальное использование
- Веб UI по умолчанию слушает `0.0.0.0:8080` — при необходимости сменить на `127.0.0.1:8080`

### Android безопасность

- VpnService API — официальный Android механизм, не требует root
- Socket protection предотвращает routing loop (весь bypass-трафик идёт мимо TUN)
- Foreground Service + уведомление — пользователь всегда знает что VPN активен
- DNS защита: все DNS запросы перехватываются и резолвятся через DoH — ни один запрос не уходит открыто к провайдеру

### Fake packets и привилегии

- `fake` стратегия требует `CAP_NET_RAW`
- В Docker: `cap_add: [NET_ADMIN, NET_RAW]`
- В Linux: `sudo setcap cap_net_raw+ep /usr/local/bin/freenet`
- Без привилегий стратегия автоматически fallback на `split`

### Приватность DNS

| Метод | Что видит провайдер |
|-------|---------------------|
| Обычный DNS (UDP:53) | Все запросы в открытом виде |
| DNS-over-TLS (DoT) | Зашифровано, но DoT-трафик виден на порту 853 |
| DNS-over-HTTPS (DoH) | Только HTTPS соединение к 1.1.1.1/8.8.8.8/9.9.9.9 |

FreeNet использует DoH — максимальная приватность DNS без изменения сетевых настроек ОС.

### Что FreeNet НЕ делает

- Не шифрует сам интернет-трафик (это не VPN)
- Не скрывает IP-адрес пользователя от целевых сайтов
- Не защищает от трекинга или cookies
- Не обходит авторизацию на сайтах

FreeNet только помогает установить соединение с заблокированным ресурсом, обходя DPI-блокировку по SNI и DNS-подмену.
