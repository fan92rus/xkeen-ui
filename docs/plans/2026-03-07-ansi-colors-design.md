# ANSI Colors in Command Output

## Problem

XKeen CLI commands output text with ANSI escape codes for colors (e.g., `[91m` for red, `[0m` for reset). These codes appear as raw text in the web UI modal output instead of being rendered as colors.

Example: `Прокси-клиент [91mостановлен[0m` should display "остановлен" in red.

## Solution

Use ansi_up library to convert ANSI escape codes to HTML spans with color styles. The library is small (~5KB minified) and handles the conversion client-side.

## Architecture

```
xkeen-go/web/
├── static/js/libs/
│   └── ansi_up.min.js          # Minified library
└── static/js/components/
    └── commands.js              # Import and use ansi_up
```

## Implementation Details

### 1. Add ansi_up.min.js

- Download minified ansi_up from CDN or npm
- Place in `xkeen-go/web/static/js/libs/`
- File size: ~5KB

### 2. Update index.html

Add script tag to load ansi_up before Alpine.js components:

```html
<script src="/static/js/libs/ansi_up.min.js"></script>
```

### 3. Update commands.js

```javascript
// Use ansi_up to convert output
handleStreamMessage(msg) {
    if (msg.type === 'output') {
        // Convert ANSI to HTML
        const html = ansi_up.ansi_to_html(msg.text, { use_classes: false });
        this.$store.app.modal.output += html + '\n';
    }
    // ...
}
```

### 4. Update Modal Template

Change `x-text` to `x-html` for output display:

```html
<pre id="modal-output" x-html="output"></pre>
```

## Security Considerations

- Output comes from whitelisted XKeen commands only (controlled on backend)
- No user input is directly rendered
- ANSI conversion is safe (produces only color spans)

## Files Changed

1. `xkeen-go/web/static/js/libs/ansi_up.min.js` - new file
2. `xkeen-go/web/index.html` - add script tag
3. `xkeen-go/web/static/js/components/commands.js` - use ansi_up

## Testing

1. Run `xkeen -status` command
2. Verify colored text displays correctly
3. Verify no raw ANSI codes visible
