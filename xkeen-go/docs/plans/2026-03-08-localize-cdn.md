# CDN Localization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Переместить CodeMirror и Alpine.js из CDN (esm.sh) в локальные файлы для работы офлайн.

**Architecture:** Скачать ESM-бандлы с esm.sh в `web/static/vendor/`, обновить importmap и импорты в index.html.

**Tech Stack:** Go (embed), ES Modules, esm.sh bundler

---

## Task 1: Создать структуру директорий для vendor файлов

**Files:**
- Create: `xkeen-go/web/static/vendor/` (директория)

**Step 1: Создать структуру папок**

```bash
cd xkeen-go/web/static
mkdir -p vendor/codemirror/6.0.1
mkdir -p vendor/@codemirror/lang-json/6.0.1
mkdir -p vendor/@codemirror/theme-one-dark/6.1.2
mkdir -p vendor/@codemirror/merge/6.0.0
mkdir -p vendor/@codemirror/state/6.4.1
mkdir -p vendor/@codemirror/view/6.26.3
mkdir -p vendor/alpinejs/3.14.3
```

**Step 2: Проверить структуру**

Run: `ls -R xkeen-go/web/static/vendor/`
Expected: Все папки созданы

**Step 3: Commit**

```bash
git add xkeen-go/web/static/vendor/.gitkeep
git commit -m "chore: create vendor directory structure for CDN packages"
```

---

## Task 2: Скачать CodeMirror core бандл

**Files:**
- Create: `xkeen-go/web/static/vendor/codemirror/6.0.1/index.js`

**Step 1: Скачать бандл**

```bash
cd xkeen-go/web/static/vendor/codemirror/6.0.1
curl -L "https://esm.sh/codemirror@6.0.1?bundle" -o index.js
```

**Step 2: Проверить файл**

Run: `head -20 xkeen-go/web/static/vendor/codemirror/6.0.1/index.js`
Expected: JavaScript код без ошибок

**Step 3: Commit**

```bash
git add xkeen-go/web/static/vendor/codemirror/
git commit -m "vendor: add CodeMirror 6.0.1 bundle"
```

---

## Task 3: Скачать CodeMirror плагины

**Files:**
- Create: `xkeen-go/web/static/vendor/@codemirror/*/index.js` (5 файлов)

**Step 1: Скачать lang-json**

```bash
cd xkeen-go/web/static/vendor/@codemirror/lang-json/6.0.1
curl -L "https://esm.sh/@codemirror/lang-json@6.0.1?bundle" -o index.js
```

**Step 2: Скачать theme-one-dark**

```bash
cd xkeen-go/web/static/vendor/@codemirror/theme-one-dark/6.1.2
curl -L "https://esm.sh/@codemirror/theme-one-dark@6.1.2?bundle" -o index.js
```

**Step 3: Скачать merge**

```bash
cd xkeen-go/web/static/vendor/@codemirror/merge/6.0.0
curl -L "https://esm.sh/@codemirror/merge@6.0.0?bundle" -o index.js
```

**Step 4: Скачать state**

```bash
cd xkeen-go/web/static/vendor/@codemirror/state/6.4.1
curl -L "https://esm.sh/@codemirror/state@6.4.1?bundle" -o index.js
```

**Step 5: Скачать view**

```bash
cd xkeen-go/web/static/vendor/@codemirror/view/6.26.3
curl -L "https://esm.sh/@codemirror/view@6.26.3?bundle" -o index.js
```

**Step 6: Проверить размеры файлов**

Run: `du -sh xkeen-go/web/static/vendor/@codemirror/*/`
Expected: Каждый файл 50-200KB

**Step 7: Commit**

```bash
git add xkeen-go/web/static/vendor/@codemirror/
git commit -m "vendor: add CodeMirror plugins (lang-json, theme-one-dark, merge, state, view)"
```

---

## Task 4: Скачать Alpine.js бандл

**Files:**
- Create: `xkeen-go/web/static/vendor/alpinejs/3.14.3/index.js`

**Step 1: Скачать бандл**

```bash
cd xkeen-go/web/static/vendor/alpinejs/3.14.3
curl -L "https://esm.sh/alpinejs@3.14.3?bundle" -o index.js
```

**Step 2: Проверить файл**

Run: `head -20 xkeen-go/web/static/vendor/alpinejs/3.14.3/index.js`
Expected: JavaScript код Alpine.js

**Step 3: Commit**

```bash
git add xkeen-go/web/static/vendor/alpinejs/
git commit -m "vendor: add Alpine.js 3.14.3 bundle"
```

---

## Task 5: Обновить importmap в index.html

**Files:**
- Modify: `xkeen-go/web/index.html:445-456`

**Step 1: Обновить importmap**

Заменить:
```html
    <script type="importmap">
    {
        "imports": {
            "codemirror": "https://esm.sh/codemirror@6.0.1",
            "@codemirror/lang-json": "https://esm.sh/@codemirror/lang-json@6.0.1",
            "@codemirror/theme-one-dark": "https://esm.sh/@codemirror/theme-one-dark@6.1.2",
            "@codemirror/merge": "https://esm.sh/@codemirror/merge@6.0.0",
            "@codemirror/state": "https://esm.sh/@codemirror/state@6.4.1",
            "@codemirror/view": "https://esm.sh/@codemirror/view@6.26.3"
        }
    }
    </script>
```

На:
```html
    <script type="importmap">
    {
        "imports": {
            "codemirror": "/static/vendor/codemirror/6.0.1/index.js",
            "@codemirror/lang-json": "/static/vendor/@codemirror/lang-json/6.0.1/index.js",
            "@codemirror/theme-one-dark": "/static/vendor/@codemirror/theme-one-dark/6.1.2/index.js",
            "@codemirror/merge": "/static/vendor/@codemirror/merge/6.0.0/index.js",
            "@codemirror/state": "/static/vendor/@codemirror/state/6.4.1/index.js",
            "@codemirror/view": "/static/vendor/@codemirror/view/6.26.3/index.js"
        }
    }
    </script>
```

**Step 2: Commit**

```bash
git add xkeen-go/web/index.html
git commit -m "feat: use local CodeMirror vendor files instead of CDN"
```

---

## Task 6: Обновить импорт Alpine.js в index.html

**Files:**
- Modify: `xkeen-go/web/index.html:469`

**Step 1: Обновить импорт Alpine.js**

Заменить:
```javascript
        import Alpine from 'https://esm.sh/alpinejs@3.14.3';
```

На:
```javascript
        import Alpine from '/static/vendor/alpinejs/3.14.3/index.js';
```

**Step 2: Commit**

```bash
git add xkeen-go/web/index.html
git commit -m "feat: use local Alpine.js vendor file instead of CDN"
```

---

## Task 7: Собрать и протестировать

**Files:**
- Modify: бинарник (пересборка)

**Step 1: Собрать проект**

```bash
cd xkeen-go
make build
```

Expected: Успешная сборка, размер бинаря увеличился на ~400-600KB

**Step 2: Запустить локально**

```bash
make run
```

**Step 3: Проверить в браузере**

1. Открыть http://localhost:8089
2. Зайти под админом
3. Открыть вкладку Editor
4. Проверить что редактор CodeMirror работает
5. Проверить подсветку JSON
6. Проверить что Alpine.js работает (переключение табов)

**Step 4: Проверить сетевые запросы**

В DevTools → Network:
- Не должно быть запросов к esm.sh
- Все JS файлы должны загружаться с `/static/vendor/`

**Step 5: Commit (если всё ок)**

```bash
git add -A
git commit -m "test: verify local vendor packages work correctly"
```

---

## Task 8: Удалить пустые папки .gitkeep если есть

**Files:**
- Modify: `xkeen-go/web/static/vendor/`

**Step 1: Проверить наличие .gitkeep**

Run: `find xkeen-go/web/static/vendor -name ".gitkeep"`

**Step 2: Удалить если есть**

```bash
find xkeen-go/web/static/vendor -name ".gitkeep" -delete
```

**Step 3: Commit если были изменения**

```bash
git add -A
git commit -m "chore: remove unnecessary .gitkeep files"
```

---

## Summary

После выполнения всех задач:
- CodeMirror 6 и Alpine.js загружаются локально
- Приложение работает без интернета
- Размер бинаря увеличен на ~400-600KB
- Все функции редактора сохранены
