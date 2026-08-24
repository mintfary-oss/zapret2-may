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
│   ├── proxy/            Перехват трафика
│   │   ├── socks5        SOCKS5 прокси (RFC 1928)               ✅
│   │   ├── transparent   iptables REDIRECT (Linux)              ✅
│   │   └── nfqueue       Netfilter queue (Linux, ядро)          ✅
│   │
│   └── lists/            Списки доменов
│       ├── hostlist       Загрузка из файла + wildcard           ✅
│       └── antifilter     antifilter.download (800K+ доменов)   ✅
│
├── Платформы
│   ├── Linux/Docker      systemd сервис + Docker Compose        ✅
│   ├── Windows           Windows Service + exe                  ✅
│   ├── Android           VpnService APK (без root)              ✅
│   └── OpenWrt           init.d скрипт                         🔄
│
└── Интерфейсы
    ├── web/              Веб-UI + WebSocket логи                ✅
    ├── android/          Compose UI (кнопка, лог, статистика)   ✅
    ├── tray/             Системный трей (Windows/Linux)         🔄
    └── telegram/         Telegram бот управление                🔄
```

Обозначения: ✅ готово · 🔄 в плане

---

## Фазы разработки

### Фаза 1: Основа ✅ ЗАВЕРШЕНА

- Go модуль, структура пакетов
- SOCKS5 прокси + прозрачный прокси + nfqueue
- Стратегии: split, disorder, QUIC, TLS record, fake, combined, auto
- TLS ClientHello парсер + SNI extraction
- Hostlist с авто-обновлением
- Веб UI с большой кнопкой + WebSocket логи в реальном времени
- Docker Compose + Dockerfile
- Linux systemd установщик

### Фаза 2: Усиление bypass ✅ ЗАВЕРШЕНА

- Fake packets через raw sockets (Linux `SOCK_RAW`, TTL=4, bad checksum)
- TLS record layer splitting (2 TLS записи вместо одной)
- nfqueue интеграция для Linux
- Combined стратегия (fake + tlsrec)
- Windows кросс-компиляция
- GitHub Actions CI/CD

### Фаза 3: Android APK ✅ ЗАВЕРШЕНА

- `mobile/` пакет — gomobile-bindable Go API
- gomobile bind → `.aar` библиотека
- Android Studio проект (Kotlin/Jetpack Compose)
- VpnService API (без root)
- TUN packet forwarder (Go + Kotlin fallback)
- Per-boot автозапуск
- GitHub Release автопубликация артефактов

### Фаза 4: Windows GUI 🔄 СЛЕДУЮЩАЯ

- Системный трей (`github.com/getlantern/systray`)
- WinDivert интеграция (CGO, dll уже есть в репо `nfq2/windows/`)
- NSIS установщик → `freenet-setup.exe`
- Автозапуск через Windows Service (уже есть базовая реализация)

### Фаза 5: ECH + качество 🔄

- ECH (Encrypted Client Hello) — шифрует SNI в TLS 1.3
- Unit тесты для стратегий bypass
- Integration тесты с mock DPI сервером
- Бенчмарки производительности

### Фаза 6: Android улучшения 🔄

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
| Windows транспорт | WinDivert (CGO) | Уже в репо, проверен |
| Конфиг | YAML | Читаемый, стандарт де-факто |
| Логи | ring buffer + WebSocket | Real-time, нет disk I/O |
| Списки блокировок | antifilter.download | 800K+ доменов, обновляется ежедневно |
| CI/CD | GitHub Actions | Авто-релиз для всех платформ |
