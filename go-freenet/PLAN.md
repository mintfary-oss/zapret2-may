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
| ECH | ❌ | ❌ | ❌ | ✅ |
| Auto-detect ISP | ❌ | ❌ | ❌ | ✅ |
| DNS-over-HTTPS | ❌ | ❌ | ❌ | ✅ |
| Системный трей | ✅ | ❌ | ❌ | ✅ |
| Авто системный прокси | ❌ | ❌ | ❌ | ✅ Windows |
| WinDivert (ядро) | ✅ | ❌ | ❌ | ✅ |
| Per-app фильтрация | ❌ | ❌ | ❌ | ✅ Android |
| Home screen widget | ❌ | ❌ | ❌ | ✅ Android |
| UDP relay (Discord/Steam) | ❌ | ❌ | ❌ | ✅ Android |
| Веб UI | ❌ | ❌ | ❌ | ✅ |
| Docker | ❌ | ❌ | ❌ | ✅ |
| GitHub Release (auto) | ❌ | ❌ | ❌ | ✅ |
| Один бинарник | ✅ | ✅ | ❌ | ✅ |
| Язык | C | C | C+Lua | **Go** |

---

## Архитектура

```
FreeNet Go
│
├── Ядро (Go)
│   ├── bypass/           Стратегии обхода DPI
│   │   ├── split         TCP фрагментация на позиции SNI         ✅
│   │   ├── disorder      Disorder атака (best-effort)            ✅
│   │   ├── tlsrec        TLS record layer splitting              ✅
│   │   ├── fake          Фейковые пакеты (raw socket, Linux)     ✅
│   │   ├── combined      fake + tlsrec                           ✅
│   │   ├── quic          QUIC Initial фрагментация               ✅
│   │   ├── ech           ECH detect + passthrough                ✅
│   │   └── autodetect    Авто-выбор стратегии под ISP            ✅
│   │
│   ├── dns/              DNS-over-HTTPS защита
│   │   ├── doh           DoH-клиент RFC 8484 + ECH               ✅
│   │   └── resolver      Локальный UDP→DoH резолвер              ✅
│   │
│   ├── proxy/            Перехват трафика
│   │   ├── socks5        SOCKS5 прокси (RFC 1928)                ✅
│   │   ├── transparent   iptables REDIRECT (Linux)               ✅
│   │   └── nfqueue       Netfilter queue (Linux, ядро)           ✅
│   │
│   ├── windivert/        Windows kernel bypass
│   │   └── windows       syscall.NewLazyDLL + пакет split        ✅
│   │
│   ├── sysproxy/         Системный прокси
│   │   └── windows       Реестр HKCU/Internet Settings           ✅
│   │
│   └── lists/            Списки доменов
│       ├── hostlist       Загрузка из файла + wildcard            ✅
│       └── antifilter     antifilter.download (800K+ доменов)    ✅
│
├── Платформы
│   ├── Linux/Docker      systemd сервис + Docker Compose         ✅
│   ├── Windows           WinDivert + служба + трей + авто-прокси ✅
│   ├── Android           VpnService + UDP relay + per-app + widget ✅
│   └── OpenWrt           init.d скрипт                          🔄
│
└── Интерфейсы
    ├── web/              Веб-UI + WebSocket + DoH + ECH статус   ✅
    ├── android/          Compose UI + SplitTunnelCard + Widget   ✅
    └── telegram/         Telegram бот управление                 🔄
```

Обозначения: ✅ готово · 🔄 в плане

---

## Фазы разработки

### Фаза 1: Основа ✅ ЗАВЕРШЕНА (v1.0.5)

- Go модуль, структура пакетов
- SOCKS5 прокси + прозрачный прокси + nfqueue (Linux)
- Стратегии: split, disorder, QUIC, TLS record, fake, combined, auto
- TLS ClientHello парсер + SNI extraction
- Hostlist с авто-обновлением
- Веб UI с большой кнопкой + WebSocket логи в реальном времени
- Docker Compose + Dockerfile
- Linux systemd установщик
- GitHub Actions CI/CD + авто-релиз + страница скачивания

### Фаза 2: Усиление bypass ✅ ЗАВЕРШЕНА (v1.0.5)

- Fake packets через raw sockets (Linux `SOCK_RAW`, TTL=4, bad checksum)
- TLS record layer splitting
- Combined стратегия (fake + tlsrec)
- Windows кросс-компиляция (CGO_ENABLED=0)

### Фаза 3: Android APK ✅ ЗАВЕРШЕНА (v1.0.5)

- `mobile/` пакет — gomobile-bindable Go API
- gomobile bind → `.aar` библиотека
- Android Studio проект (Kotlin/Jetpack Compose)
- VpnService API (без root)
- Per-boot автозапуск
- GitHub Release автопубликация артефактов

### Фаза 4а: Windows GUI ✅ ЗАВЕРШЕНА (v1.1.0)

- Системный трей (`github.com/getlantern/systray`)
- Меню: статус, стратегии, открыть веб UI, выйти

### Фаза 4б: Windows авто-прокси ✅ ЗАВЕРШЕНА (v1.2.0)

- Запись SOCKS5 прокси в реестр Windows (HKCU/Internet Settings)
- `WM_SETTINGCHANGE` broadcast — браузеры реагируют без перезапуска

### Фаза 4в: DNS-over-HTTPS ✅ ЗАВЕРШЕНА (v1.3.0)

- DoH-клиент RFC 8484 (Cloudflare/Google/Quad9)
- Локальный UDP-резолвер `127.0.0.1:5300`
- Android TUN перехват UDP:53 → DoH
- Статус в веб UI

### Фаза 5: ECH + Unit Tests ✅ ЗАВЕРШЕНА (v1.4.0)

- ECH обнаружение (`0xFE0D`) → passthrough
- HTTPS DNS lookup для ECH config (RFC 9460)
- `EnableECH()` в DoH HTTP транспорте
- Статус `🔐 ECH обнаружен: N соед.` в веб UI
- 35 unit тестов (bypass/dns/logs)

### Фаза 6: Windows WinDivert ✅ ЗАВЕРШЕНА (v1.5.2)

- `internal/windivert/` — DLL loader без CGO (`syscall.NewLazyDLL`)
- Перехват исходящего TCP:443 на уровне ядра
- Применяет split/tlsrec/combined прямо в пакетах
- ECH passthrough (не трогает уже-зашифрованный SNI)
- CI: скачивает WinDivert latest → `freenet-windows-bundle.zip`
- PowerShell installer обновлён: скачивает bundle
- Трей: `⚡ WinDivert: активен (все приложения)`

### Фаза 7: Android улучшения ✅ ЗАВЕРШЕНА (v1.6.0)

- `mobile/tun_udp.go` — UDP NAT relay (Discord, Steam, игры, QUIC)
- IPv6 исключён из VPN маршрутов (IPv4-only форвардер)
- `SplitTunnelConfig.kt` — per-app split tunnel (SharedPreferences)
- `FreenetVpnService.kt` — `addAllowedApplication()` / `addDisallowedApplication()`
- `FreeNetWidget.kt` — 2×1 кнопка на рабочем столе
- `SplitTunnelCard` в Compose UI — список приложений с поиском

---

## Следующие фазы (планируется)

### Фаза 8: Качество и F-Droid 🔄

- F-Droid манифест (`metadata/com.freenet.vpn.yml`)
- F-Droid build — сборка без gomobile AAR (pure Java fallback)
- Android integration tests
- Бенчмарки производительности bypass
- OpenWrt init.d скрипт

### Фаза 9: Telegram бот 🔄

- `internal/telegram/` — бот через Bot API
- Команды: `/status`, `/enable`, `/disable`, `/strategy auto`
- Управление удалённым сервером из телефона
- Push-уведомления о смене статуса

---

## Технологии

| Компонент | Технология | Обоснование |
|-----------|-----------|-------------|
| Язык | Go 1.26 | Кросс-платформа, один бинарник, gomobile |
| Android UI | Kotlin/Compose | Нативный, современный Android |
| Android транспорт | gomobile + VpnService | Без root, официальное API |
| DNS защита | RFC 8484 DoH | Стандарт, HTTPS, без root |
| ECH | RFC 9601 + RFC 9460 | Новейшая защита SNI (2026) |
| Windows kernel | WinDivert 2.x (syscall) | Перехват без CGO |
| Windows трей | getlantern/systray | Нативный трей Win/Lin/Mac |
| Windows прокси | HKCU Registry | Авто-настройка без admin прав |
| Android split tunnel | VpnService.Builder | Нативное Android API |
| Android widget | AppWidgetProvider | Стандарт Android |
| Конфиг | YAML | Читаемый, стандарт де-факто |
| Логи | ring buffer + WebSocket | Real-time, нет disk I/O |
| Списки | antifilter.download | 800K+ доменов, ежедневно |
| CI/CD | GitHub Actions | Авто-релиз для всех платформ |
