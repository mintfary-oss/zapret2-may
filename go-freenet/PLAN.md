# План проекта — FreeNet Go 2026

## Цель

Создать лучший кросс-платформенный инструмент обхода DPI для России (РКН/ТСПУ),
написанный на Go. Конкурировать с GoodbyeDPI, ByeDPI, Zapret и превзойти их
по совокупности возможностей.

---

## Сравнение с конкурентами

| Функция | GoodbyeDPI | ByeDPI | Zapret | **FreeNet Go** |
|---------|------------|--------|--------|----------------|
| Linux | ❌ | ❌ | ✅ | ✅ |
| Windows | ✅ | ❌ | частично | ✅ (план) |
| Android | ❌ | ✅ | частично | ✅ (план) |
| QUIC/HTTP3 | ❌ | ❌ | ✅ | ✅ |
| ECH | ❌ | ❌ | ❌ | ✅ (план) |
| Auto-detect | ❌ | ❌ | ❌ | ✅ |
| Веб UI | ❌ | ❌ | ❌ | ✅ |
| Docker | ❌ | ❌ | ❌ | ✅ |
| Один бинарник | ✅ | ✅ | ❌ | ✅ |
| Язык | C | C | C+Lua | **Go** |

---

## Архитектура (полная)

```
FreeNet Go
│
├── Ядро (Go) ─────────────────────────────────────────────────────────────────
│   ├── bypass/           Стратегии обхода DPI
│   │   ├── split         TCP фрагментация на позиции SNI ✅
│   │   ├── disorder      Disorder атака (best-effort) ✅
│   │   ├── fake          Фейковые пакеты (нужен raw socket) 🔄
│   │   ├── quic          QUIC Initial фрагментация ✅
│   │   ├── ech           Encrypted Client Hello (TLS 1.3) 🔄
│   │   ├── tls_record    TLS record splitting 🔄
│   │   └── autodetect    Авто-выбор стратегии ✅
│   │
│   ├── proxy/            Перехват трафика
│   │   ├── socks5        SOCKS5 прокси ✅
│   │   ├── transparent   iptables REDIRECT (Linux) ✅
│   │   ├── tun           TUN интерфейс (Android/Linux) 🔄
│   │   └── windivert     WinDivert (Windows) 🔄
│   │
│   └── lists/            Списки доменов
│       ├── hostlist       Загрузка из файла ✅
│       ├── antizapret     antizapret.prostovpn.com 🔄
│       └── antifilter     antifilter.download ✅
│
├── Интерфейсы ─────────────────────────────────────────────────────────────────
│   ├── web/              Веб-UI (Go HTTP) ✅
│   ├── tray/             Системный трей (Windows/Linux) 🔄
│   ├── android/          Android APK (gomobile) 🔄
│   └── telegram/         Telegram бот управление 🔄
│
└── Развёртывание ──────────────────────────────────────────────────────────────
    ├── docker-compose    ✅
    ├── systemd           ✅
    ├── windows installer NSIS/Inno Setup 🔄
    ├── android APK       gomobile 🔄
    └── openwrt           init.d скрипт 🔄
```

Обозначения: ✅ готово · 🔄 в плане

---

## Фазы разработки

### Фаза 1: Основа (ВЫПОЛНЕНО) ✅
- Go модуль, структура пакетов
- SOCKS5 прокси + прозрачный прокси
- Стратегии: split, disorder, QUIC
- TLS ClientHello парсер
- Hostlist с авто-обновлением
- Веб UI с большой кнопкой + WebSocket логи
- Docker Compose
- Linux systemd установщик

### Фаза 2: Усиление bypass (следующая) 🔄
- Fake packets через raw sockets (Linux `SOCK_RAW`)
- TLS record layer splitting (разбивка на уровне TLS record)
- ECH (Encrypted Client Hello) — новейшая техника
- nfqueue интеграция (как в zapret2) для Linux
- JA3/JA4 fingerprint spoofing

### Фаза 3: Windows 🔄
- Интеграция WinDivert (C библиотека уже есть в репо)
- Go + CGO обёртка для WinDivert
- Системный трей (fyne.io или systray)
- NSIS установщик → `freenet-setup.exe`
- Автозапуск через Windows Service

### Фаза 4: Android 🔄
- gomobile bind → `.aar` библиотека
- Android Studio проект (Kotlin/Compose)
- VpnService API (без root)
- gVisor netstack + tun2socks
- Per-app фильтрация
- Публикация APK в релизы GitHub

### Фаза 5: Качество 🔄
- Unit тесты для bypass стратегий
- Integration тесты с mock DPI
- CI/CD (GitHub Actions: build + test)
- Кросс-компиляция: linux/amd64, linux/arm64, windows/amd64, android/arm64
- Автоматические релизы с артефактами

---

## Технологии

| Компонент | Технология | Обоснование |
|-----------|-----------|-------------|
| Язык | Go 1.22+ | Кросс-платформа, один бинарник, gomobile |
| Android UI | Kotlin/Compose | Нативный Android |
| Android transport | gomobile + VpnService | Без root |
| Windows transport | WinDivert (CGO) | Уже в репо, проверен |
| Windows UI | fyne.io или systray | Чистый Go |
| Конфиг | YAML | Читаемый, стандарт |
| Логи | ring buffer + WebSocket | Real-time без накладных расходов |
| Списки | antifilter.download | 800K+ доменов, обновляется ежедневно |
