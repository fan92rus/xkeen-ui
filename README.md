# XKEEN-UI

Веб-интерфейс для управления конфигурациями сетевых сервисов на роутерах Keenetic с установленным Entware.

## Быстрая установка

Выполните на роутере одну команду:

```bash
wget -qO- https://api.github.com/repos/fan92rus/xkeen-ui/releases/latest | grep "browser_download_url.*arm64" | cut -d '"' -f 4 | xargs -r wget -qO xkeen-ui-keenetic-arm64 && chmod +x xkeen-ui-keenetic-arm64 && ./xkeen-ui-keenetic-arm64 install
```

После установки запустите сервис:

```bash
xkeen-ui start
```

Откройте веб-интерфейс: `http://<ip-роутера>:8089`

**Пароль по умолчанию:** `admin`

> ⚠️ **Важно:** Система потребует сменить пароль при первом входе.

## Требования

- Роутер Keenetic с установленным [Entware](https://github.com/Entware/Entware/wiki)
- Установленный XKeen
- Архитектура: ARM64 (KN-1010, KN-1810, KN-1910 и др.) или MIPS

## Возможности

### Редактор конфигураций

- Подсветка синтаксиса JSON/YAML с темой One Dark
- Поддержка JSONC (JSON с комментариями)
- Автоматическое создание резервных копий при сохранении
- Просмотр различий (diff) между версиями
- Восстановление из резервных копий

### Управление сервисами

- Запуск, остановка, перезапуск
- Мониторинг статуса в реальном времени
- Переключение между ядрами Xray и Mihomo

### Просмотр логов

- Потоковая передача логов через WebSocket
- Фильтрация по уровню (error, warn, info)
- Поиск по содержимому логов
- Переключение между access.log и error.log

### Консоль команд XKeen

- Интерактивное выполнение команд с выводом в реальном времени
- Поддержка ввода данных для интерактивных команд
- Категоризация команд по группам
- Предупреждения для опасных операций

### Настройки

- Переключение режима работы (Xray/Mihomo)
- Изменение уровня логирования
- Управление паролем администратора
- Проверка и установка обновлений в один клик

## Команды управления сервисом

```bash
xkeen-ui start    # Запуск
xkeen-ui stop     # Остановка
xkeen-ui restart  # Перезапуск
xkeen-ui status   # Статус
xkeen-ui log      # Логи
```

## Конфигурация

Файл конфигурации: `/opt/etc/xkeen-ui/config.json`

```json
{
    "port": 8089,
    "mode": "xray",
    "xray_config_dir": "/opt/etc/xray/configs",
    "xkeen_binary": "xkeen",
    "mihomo_config_dir": "/opt/etc/mihomo",
    "mihomo_binary": "mihomo",
    "allowed_roots": [
        "/opt/etc/xray",
        "/opt/etc/xkeen",
        "/opt/etc/mihomo",
        "/opt/var/log"
    ],
    "log_level": "info",
    "auth": {
        "session_timeout": 24,
        "max_login_attempts": 5,
        "lockout_duration": 5
    }
}
```

### Параметры

| Параметр | Описание | По умолчанию |
|----------|----------|--------------|
| `port` | Порт веб-интерфейса | 8089 |
| `mode` | Режим работы: `xray` или `mihomo` | xray |
| `xray_config_dir` | Директория конфигураций Xray | /opt/etc/xray/configs |
| `mihomo_config_dir` | Директория конфигураций Mihomo | /opt/etc/mihomo |
| `allowed_roots` | Разрешённые директории для файловых операций | - |
| `log_level` | Уровень логирования (debug, info, warn, error) | info |
| `session_timeout` | Время жизни сессии в часах | 24 |
| `max_login_attempts` | Попыток входа до блокировки | 5 |
| `lockout_duration` | Длительность блокировки в минутах | 5 |

## Безопасность

- **bcrypt** — хеширование паролей с cost 12
- **CSRF** — защита от межсайтовой подделки запросов
- **Rate limiting** — ограничение попыток входа по IP
- **Path validation** — защита от path traversal через whitelist директорий
- **Security headers** — X-Frame-Options, CSP, X-Content-Type-Options

## Удаление

```bash
xkeen-ui uninstall
```

При удалении будет предложено сохранить директорию конфигурации.

## Сборка из исходного кода

```bash
git clone https://github.com/fan92rus/xkeen-ui.git
cd xkeen-ui/xkeen-go

make deps                  # Установка зависимостей
make build                 # Сборка для текущей ОС
make keenetic-arm64        # Сборка для Keenetic ARM64

# Опционально: сжатие UPX
upx --best --lzma build/xkeen-ui-keenetic-arm64
```

## Технологии

- **Backend:** Go 1.21+
- **Frontend:** Alpine.js, CodeMirror 6
- **Протоколы:** HTTP, WebSocket, SSE

## Лицензия

MIT License

## Автор

[fan92rus](https://github.com/fan92rus)
