# Что сделано — FreeNet Go

## Статус: v0.2.0 — Phase 2 завершена

---

## Структура проекта

```
go-freenet/
├── cmd/freenet/main.go                   # точка входа, CLI флаги
├── internal/
│   ├── bypass/
│   │   ├── engine.go                     # выбор и запуск стратегии
│   │   ├── split.go                      # TCP фрагментация (split)
│   │   ├── disorder.go                   # disorder атака
│   │   ├── tlsrec.go                     # TLS record layer splitting ✨ NEW
│   │   ├── fake.go                       # fake packets — интерфейс ✨ NEW
│   │   ├── fake_linux.go                 # fake packets — raw socket (Linux) ✨ NEW
│   │   ├── fake_stub.go                  # fake packets — заглушка (не-Linux) ✨ NEW
│   │   ├── quic.go                       # QUIC/HTTP3 bypass
│   │   ├── tls.go                        # парсер TLS ClientHello / SNI
│   │   ├── autodetect.go                 # авто-подбор стратегии
│   │   └── hostlist.go                   # фильтрация по доменам
│   ├── config/config.go                  # YAML конфигурация (+ NFQueueConfig) ✨ NEW
│   ├── logs/ring.go                      # кольцевой буфер логов
│   ├── proxy/
│   │   ├── server.go                     # управление сервером (+ NFQueue) ✨ NEW
│   │   ├── socks5.go                     # SOCKS5 прокси (RFC 1928)
│   │   ├── transparent.go                # прозрачный прокси (Linux)
│   │   ├── transparent_stub.go           # заглушка (не-Linux) ✨ NEW
│   │   ├── nfqueue.go                    # netfilter queue (Linux) ✨ NEW
│   │   ├── nfqueue_stub.go               # заглушка (не-Linux) ✨ NEW
│   │   └── stats.go                      # статистика соединений
│   ├── types/types.go                    # общие типы данных
│   └── web/ui.go                         # веб-интерфейс + WebSocket
├── init.d/systemd/freenet.service        # systemd unit
├── scripts/
│   ├── install.sh                        # установщик Linux
│   ├── setup-transparent.sh              # настройка iptables
│   └── teardown-transparent.sh           # откат iptables
├── .github/workflows/build.yml           # CI/CD ✨ NEW
├── dist/                                 # скомпилированные бинарники
│   ├── freenet-linux-amd64
│   ├── freenet-linux-arm64
│   └── freenet-windows-amd64.exe
├── Dockerfile                            # multi-stage build
├── docker-compose.yml                    # одна команда запуска
├── docker-entrypoint.sh                  # entrypoint с iptables
└── go.mod / go.sum                       # зависимости
```

---

## Реализованные стратегии обхода DPI

| Стратегия | Описание | Требования | Эффективность |
|-----------|----------|------------|---------------|
| **split** | TCP фрагментация ClientHello на позиции SNI | нет | Средняя |
| **disorder** | Перестановка сегментов head/tail | нет | Средняя |
| **tlsrec** | TLS record layer splitting (2 TLS записи) | нет | Высокая |
| **fake** | Decoy пакет (TTL или bad checksum) + split | CAP_NET_RAW | Высокая |
| **combined** | fake + tlsrec — максимальный эффект | CAP_NET_RAW | Максимальная |
| **quic** | QUIC Initial фрагментация (UDP 443) | нет | Средняя |
| **auto** | Авто-тест всех стратегий, выбор лучшей | — | Адаптивная |
| **none** | Без обхода (для отладки) | — | — |

### Режимы fake packets

| Режим | Описание |
|-------|----------|
| **TTL** | Decoy с TTL=4–8 — умирает у провайдера, DPI обработает |
| **MD5** | Decoy с неверным TCP checksum — сервер дропает, DPI не проверяет |

---

## Перехват трафика

| Метод | Описание | ОС | Требования |
|-------|----------|----|------------|
| **SOCKS5** | Ручная настройка браузера/ОС | Все | нет |
| **Transparent proxy** | iptables REDIRECT → SO_ORIGINAL_DST | Linux | CAP_NET_ADMIN |
| **nfqueue** | Ядро передаёт пакеты напрямую в процесс | Linux | CAP_NET_ADMIN + CAP_NET_RAW |

### Как включить nfqueue (максимальный режим)

```bash
# 1. В config.yaml:
#    nfqueue:
#      enabled: true
#      queue_num: 200

# 2. Установить iptables правило:
iptables -A OUTPUT -p tcp --dport 443 \
  -m connbytes --connbytes 0:6 \
  --connbytes-dir original --connbytes-mode packets \
  -j NFQUEUE --queue-num 200

# 3. Запустить с правами CAP_NET_ADMIN + CAP_NET_RAW
sudo ./freenet -config config.yaml
```

---

## CI/CD — GitHub Actions

Автоматическая сборка при каждом push в ветки `master`, `main`, `neo/**`:

| Платформа | Артефакт | Размер |
|-----------|---------|--------|
| linux/amd64 | `freenet-linux-amd64` | ~7.4 МБ |
| linux/arm64 | `freenet-linux-arm64` | ~6.9 МБ |
| linux/armv7 | `freenet-linux-armv7` | ~7.1 МБ |
| windows/amd64 | `freenet-windows-amd64.exe` | ~7.3 МБ |

При создании GitHub Release бинарники автоматически прикрепляются к релизу.

---

## Технические детали

- **Язык:** Go 1.22+
- **CGO:** не требуется (CGO_ENABLED=0)
- **Зависимости:** gorilla/websocket, gopkg.in/yaml.v3, golang.org/x/sys, go-nfqueue/v2
- **Бинарник:** один файл на платформу, без runtime зависимостей

---

## Как запустить

```bash
# Docker (рекомендуется, одна команда)
cd go-freenet && docker compose up -d
# → http://localhost:8080  (веб UI)
# → 127.0.0.1:1080        (SOCKS5)

# Напрямую (Linux/Windows)
go build -o freenet ./cmd/freenet
./freenet -web :8080

# Установка как systemd сервис (Linux)
sudo bash go-freenet/scripts/install.sh

# Максимальный режим (nfqueue, всё автоматически)
sudo ./freenet -config config.yaml  # + iptables правило выше
```
