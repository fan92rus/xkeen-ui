# Design: Система бэкапов с Diff

**Дата:** 2026-03-07
**Статус:** Approved

## Обзор

Добавление UI для работы с бэкапами конфигурационных файлов: просмотр списка бэкапов, diff с текущей версией, восстановление.

## UI дизайн

### Панель редактора

Новые кнопки рядом с индикатором "Valid JSON":

```
[Valid JSON] [Diff] [Бэкапы]
```

| Кнопка | Действие |
|--------|----------|
| **Diff** | Показать изменения между текущим содержимым редактора и последним сохранением |
| **Бэкапы** | Открыть модалку со списком бэкапов |

### Модалка "Бэкапы"

```
┌─────────────────────────────────────────────────┐
│  Бэкапы: config.json                        [X] │
├─────────────────────────────────────────────────┤
│  ● 2026-03-07 14:32:15    [Копировать][В редактор] │
│  ○ 2026-03-07 12:15:00    [Копировать][В редактор] │
│  ○ 2026-03-06 18:45:30    [Копировать][В редактор] │
│  ○ 2026-03-06 09:00:00    [Копировать][В редактор] │
│  ○ 2026-03-05 16:20:45    [Копировать][В редактор] │
│                                                 │
│  ┌─────────────────────────────────────────────┐│
│  │ Diff с выбранным бэкапом (CodeMirror merge) ││
│  │ - "outbounds": [...] (удалённые)            ││
│  │ + "outbounds": [...] (добавленные)          ││
│  └─────────────────────────────────────────────┘│
└─────────────────────────────────────────────────┘
```

**Поведение:**
- При открытии первый бэкап выбран автоматически
- Diff обновляется при выборе другого бэкапа
- **Копировать** - содержимое бэкапа в буфер обмена
- **В редактор** - загрузить содержимое бэкапа в редактор (без автоматического сохранения)

### Модалка "Diff" (кнопка Diff)

- Split-view: слева текущий редактор, справа последнее сохранение
- Использует `@codemirror/merge` для визуального выделения изменений

## Backend изменения

### Новый endpoint

```
GET /api/config/backups/content?backup_path=<path>
```

Возвращает содержимое конкретного бэкапа как текст.

### Автоудаление старых бэкапов

В методе `createBackup()` добавить очистку:

```go
func (h *ConfigHandler) createBackup(filePath string) error {
    // ... создание бэкапа ...

    // Cleanup old backups (keep only 5)
    h.cleanupOldBackups(filePath, 5)
    return nil
}

func (h *ConfigHandler) cleanupOldBackups(filePath string, keep int) {
    // Получить список бэкапов для файла
    // Отсортировать по дате (новые первые)
    // Удалить все, кроме `keep` самых новых
}
```

## Frontend изменения

### Новые импорты

```javascript
"@codemirror/merge": "https://esm.sh/@codemirror/merge@6.0.0"
```

### Изменяемые файлы

| Файл | Изменение |
|------|-----------|
| `web/index.html` | + кнопки Diff/Бэкапы, + модалки, + импорт @codemirror/merge |
| `web/static/js/store.js` | + `lastSavedContent`, обновление при save |
| `web/static/js/services/config.js` | + `getBackupContent(backupPath)` |
| `web/static/js/components/editor.js` | + интеграция @codemirror/merge для diff view |

### Store изменения

```javascript
// store.js
lastSavedContent: '',  // Содержимое последнего сохранения

async saveFile() {
    // ... сохранение ...
    this.lastSavedContent = content;
}
```

### API сервис

```javascript
// config.js
async getBackupContent(backupPath) {
    const response = await fetch(
        `/api/config/backups/content?backup_path=${encodeURIComponent(backupPath)}`
    );
    return response.text();
}
```

## Diff реализация

Используем `@codemirror/merge` - встроенное расширение CodeMirror 6.

**Два режима:**

1. **Diff при редактировании** - split-view редактора с последним сохранением
2. **Diff в модалке бэкапов** - сравнение выбранного бэкапа с текущим файлом

**Преимущества @codemirror/merge:**
- Единый стиль с редактором
- Нативная интеграция
- Визуальное выделение изменений

## Существующий контекст

- Папка `backups` уже скрыта из списка файлов (фильтруется в `ListFiles()`)
- API endpoints `/api/config/backups` и `/api/config/restore` уже существуют
- Бэкапы создаются автоматически при save/delete/rename

## Файлы для изменения

### Backend
- `internal/handlers/config.go` - + endpoint content, + cleanupOldBackups

### Frontend
- `web/index.html` - UI изменения
- `web/static/js/store.js` - lastSavedContent
- `web/static/js/services/config.js` - getBackupContent
- `web/static/js/components/editor.js` - merge view интеграция
