# План проекта — FreeNet Go 2026

## Цель

Создать лучший кросс-платформенный инструмент обхода DPI для России (РКН/ТСПУ),
написанный на Go. Превзойти конкурентов по совокупности возможностей.

---

## Сравнение с конкурентами

| Функция | GoodbyeDPI | ByeDPI | Zapret | **FreeNet Go** |
|---------|------------|--------|--------|----------------|
| Linux | ❌ | ❌ | ✅ | ✅ |
| Windows | ✅ | ❌ | частично | ✅ |
| Android | ❌ | ✅ | частично | ✅ |
| QUIC/HTTP3 | ❌ | ❌ | ✅ | ✅ |
| ECH | ❌ | ❌ | ❌ | 🔄 план |
| Auto-detect ISP | ❌ | ❌ | ❌ | ✅ |
| DNS-over-HTTPS | ❌ | ❌ | ❌ | ✅ |
| Системный трей | ✅ | ❌ | ❌ | ✅ |
| Авто системный прокси | ❌ | ❌ | ❌ | ✅ Windows |
| Веб UI | ❌ | ❌ | ❌ | ✅ |
| Docker | ❌ | ❌ | ❌ | ✅ |
| GitHub Release | ❌ | ❌ | ❌ | ✅ |
| Один бинарник | ✅ | ✅ | ❌ | ✅ |
| Язык | C | C | C+Lua | **Go** |

---

## Архитектура

```
FreeNet Go
│
├── Ядро (Go)
│   ├── bypass/           Стратегии обхода DPI
│   │   ├── split         TCP фрагментация на позиции SNI        ✅
│   │   ├── disorder      Disorder атака (best-effort)           ✅
│   │   ├── tlsrec        TLS record layer splitting             ✅
│   │   ├── fake          Фейковые пакеты (raw socket)           ✅ Linux
│   │   ├── combined      fake + tlsrec                          ✅
│   │   ├── quic          QUIC Initial фрагментация              ✅
│   │   ├── autodetect    Авто-выбор стратегии под ISP           ✅
│   │   └── ech           Encrypted Client Hello (TLS 1.3)      🔄
│   │
│   ├── dns/              DNS-over-HTTPS защита
│   │   ├── doh           DoH-клиент RFC 8484                    ✅
│   │   └── resolver      Локальный UDP→DoH резолвер             ✅
│   │
│   ├── proxy/            Перехват трафика
│   │   ├── socks5        SOCKS5 прокси (RFC 1928)               ✅
│   │   ├── transparent   iptables REDIRECT (Linux)              ✅
│   │   └── nfqueue       Netfilter queue (Linux, ядро)          ✅
│   │
│   ├── sysproxy/         Системный прокси
│   │   └── windows       Реестр HKCU/Internet Settings          ✅
│   │
│   └── lists/            Списки доменов
│       ├── hostlist       Загрузка из файла + wildcard           ✅
│       └── antifilter     antifilter.download (800K+ доменов)   ✅
│
├── Платформы
│   ├── Linux/Docker      systemd сервис + Docker Compose        ✅
│   ├── Windows           Windows Service + трей + авто-прокси   ✅
│   ├── Android           VpnService APK + DoH в TUN             ✅
│   └── OpenWrt           init.d скрипт                         🔄
│
└── Интерфейсы
    ├── web/              Веб-UI + WebSocket + DoH статус        ✅
    ├── android/          Compose UI (кнопка, лог, статистика)   ✅
    └── telegram/         Telegram бот управление                🔄
```

Обозначения: ✅ готово · 🔄 в плане

---

## Фазы разработки

### Фаза 1: Основа ✅ ЗАВЕРШЕНА (v1.0.5)

- Go модуль, структура пакетов
- SOCKS5 прокси + прозрачный прокси + nfqueue
- Стратегии: split, disorder, QUIC, TLS record, fake, combined, auto
- TLS ClientHello парсер + SNI extraction
- Hostlist с авто-обновлением
- Веб UI с большой кнопкой + WebSocket логи в реальном времени
- Docker Compose + Dockerfile
- Linux systemd установщик
- GitHub Actions CI/CD + авто-релиз
- Страница скачивания в веб-UI

### Фаза 2: Усиление bypass ✅ ЗАВЕРШЕНА (v1.0.5)

- Fake packets через raw sockets (Linux `SOCK_RAW`, TTL=4, bad checksum)
- TLS record layer splitting (2 TLS записи вместо одной)
- nfqueue интеграция для Linux
- Combined стратегия (fake + tlsrec)
- Windows кросс-компиляция

### Фаза 3: Android APK ✅ ЗАВЕРШЕНА (v1.0.5)

- `mobile/` пакет — gomobile-bindable Go API
- gomobile bind → `.aar` библиотека
- Android Studio проект (Kotlin/Jetpack Compose)
- VpnService API (без root)
- TUN packet forwarder (Go + Kotlin fallback)
- Per-boot автозапуск
- GitHub Release автопубликация артефактов

### Фаза 4a: Windows GUI ✅ ЗАВЕРШЕНА (v1.1.0)

- Системный трей (`github.com/getlantern/systray`)
- Иконка в трее вместо чёрной консоли
- Меню: статус, выбор стратегии, открыть веб-интерфейс, выйти

### Фаза 4b: Windows авто-прокси ✅ ЗАВЕРШЕНА (v1.2.0)

- Автоматическая установка системного SOCKS5 прокси в реестр Windows
- При включении bypass — Chrome/Edge/Firefox работают без настройки
- При отключении — настройки восстанавливаются
- `WM_SETTINGCHANGE` broadcast — браузеры реагируют мгновенно

### Фаза 4c: DNS-over-HTTPS ✅ ЗАВЕРШЕНА (v1.3.0)

- DoH клиент RFC 8484 (Cloudflare 1.1.1.1, Google 8.8.8.8, Quad9 9.9.9.9)
- Локальный UDP резолвер `127.0.0.1:5300` — системные DNS запросы через DoH
- Android TUN перехват UDP:53 → DoH (без root, без дополнительного порта)
- Hostlist загрузка через DoH-aware HTTP клиент
- Статус DoH в веб-интерфейсе

### Фаза 5: ECH + качество 🔄 СЛЕДУЮЩАЯ

- ECH (Encrypted Client Hello) — шифрует SNI в TLS 1.3
- Unit тесты для стратегий bypass
- Integration тесты с mock DPI сервером
- Бенчмарки производительности

### Фаза 6: Windows WinDivert 🔄

- WinDivert интеграция (CGO, dll уже есть в репо `nfq2/windows/`)
- Полный перехват всего трафика на Windows (аналог nfqueue на Linux)
- Без настройки SOCKS5 — работает для всех приложений автоматически
- NSIS установщик → `freenet-setup.exe` с GUI

### Фаза 7: Android улучшения 🔄

- Замена минимального TUN стека на `xjasonlyu/tun2socks/v2` (gVisor-based)
- IPv6 поддержка
- UDP/QUIC поддержка через TUN
- Per-app фильтрация (только выбранные приложения через VPN)
- F-Droid packaging

---

## Технологии

| Компонент | Технология | Обоснование |
|-----------|-----------|-------------|
| Язык | Go 1.26 | Кросс-платформа, один бинарник, gomobile |
| Android UI | Kotlin/Compose | Нативный, современный Android |
| Android транспорт | gomobile + VpnService | Без root, официальное Android API |
| DNS защита | RFC 8484 DoH | Стандарт, HTTPS, работает без root |
| Windows трей | getlantern/systray | Нативный трей Win/Lin/Mac |
| Windows прокси | HKCU Registry | Авто-настройка без admin прав |
| Конфиг | YAML | Читаемый, стандарт де-факто |
| Логи | ring buffer + WebSocket | Real-time, нет disk I/O |
| Списки блокировок | antifilter.download | 800K+ доменов, обновляется ежедневно |
| CI/CD | GitHub Actions | Авто-релиз для всех платформ |
