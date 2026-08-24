# Что сделано — FreeNet Go

## Статус: v0.1.0 — базовая версия готова

---

## Структура проекта

```
go-freenet/
├── cmd/freenet/main.go              # точка входа, CLI флаги
├── internal/
│   ├── bypass/
│   │   ├── engine.go                # выбор и запуск стратегии
│   │   ├── split.go                 # TCP фрагментация (основной метод)
│   │   ├── disorder.go              # disorder атака
│   │   ├── quic.go                  # QUIC/HTTP3 bypass
│   │   ├── tls.go                   # парсер TLS ClientHello / SNI
│   │   ├── autodetect.go            # авто-подбор стратегии
│   │   └── hostlist.go              # фильтрация по доменам
│   ├── config/config.go             # YAML конфигурация
│   ├── logs/ring.go                 # кольцевой буфер логов
│   ├── proxy/
│   │   ├── server.go                # управление сервером
│   │   ├── socks5.go                # SOCKS5 прокси (RFC 1928)
│   │   ├── transparent.go           # прозрачный прокси (Linux)
│   │   └── stats.go                 # статистика соединений
│   ├── types/types.go               # общие типы данных
│   └── web/ui.go                    # веб-интерфейс + WebSocket
├── init.d/systemd/freenet.service   # systemd unit
├── scripts/
│   ├── install.sh                   # установщик Linux
│   ├── setup-transparent.sh         # настройка iptables
│   └── teardown-transparent.sh      # откат iptables
├── Dockerfile                       # multi-stage build
├── docker-compose.yml               # одна команда запуска
├── docker-entrypoint.sh             # entrypoint с iptables
└── go.mod / go.sum                  # зависимости
```

---

## Реализованные функции

### Стратегии обхода DPI

| Стратегия | Описание | Эффективность |
|-----------|----------|---------------|
| **split** | Фрагментация TLS ClientHello на позиции SNI | Высокая против пассивного DPI |
| **disorder** | Перестановка сегментов head/tail | Средняя |
| **quic** | Фрагментация QUIC Initial (UDP 443) | Частичная |
| **auto** | Авто-тест всех стратегий, выбор лучшей | Адаптивная |
| **none** | Без обхода (для отладки) | — |

### TLS ClientHello парсер
- Находит точную позицию SNI в пакете
- Позволяет делать split именно на границе hostname
- Работает с TLS 1.2 и TLS 1.3

### SOCKS5 прокси (RFC 1928)
- Полная поддержка: IPv4, IPv6, domain names
- Без аутентификации (no-auth)
- Счётчики активных соединений, байт, обойдённых запросов
- По умолчанию: `127.0.0.1:1080`

### Прозрачный прокси (Linux)
- Перехват через iptables REDIRECT
- Восстановление оригинального адреса через `SO_ORIGINAL_DST`
- Весь трафик обходит DPI без настройки браузера

### Hostlist (фильтрация доменов)
- Загрузка списка доменов из файла
- Авто-обновление с `antifilter.download`
- Поддержка wildcard: `youtube.com` → обходит и `www.youtube.com`

### Веб-интерфейс
- Большая круглая кнопка вкл/выкл (зелёная/красная)
- WebSocket: логи в реальном времени (последние 500 строк)
- Статистика: активные / всего / обойдено (обновляется каждые 2 сек)
- Выбор стратегии в UI
- Кнопка "Авто-детект" — тестирует стратегии по заданному хосту
- По умолчанию: `http://localhost:8080`

### Docker
- Multi-stage build (builder → alpine:3.20)
- Размер образа: ~15 МБ
- `docker compose up -d` — одна команда
- Persistent volume для конфига и списков доменов
- NET_ADMIN capability для iptables

### Linux установка
- `sudo bash scripts/install.sh` — полная установка
- Создаёт пользователя `freenet`
- Устанавливает systemd сервис
- Конфиг в `/etc/freenet/config.yaml`

---

## Технические детали

- **Язык:** Go 1.22+
- **Зависимости:** gorilla/websocket, gopkg.in/yaml.v3, golang.org/x/sys
- **Архитектуры:** amd64, arm64, armv7 (через `GOARCH=`)
- **ОС:** Linux, Windows (частично — без transparent proxy)
- **Бинарник:** один файл, без зависимостей при запуске (~8 МБ stripped)

---

## Как запустить

```bash
# Docker (рекомендуется)
cd go-freenet
docker compose up -d
# → http://localhost:8080

# Напрямую (Linux/Windows/macOS)
cd go-freenet
go build -o freenet ./cmd/freenet
./freenet -web :8080

# Установка как сервис (Linux)
sudo bash go-freenet/scripts/install.sh
```
