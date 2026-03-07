# ANSI Colors Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Render ANSI escape codes as colored text in command output modal.

**Architecture:** Import ansi_up library as ES module, convert ANSI codes to HTML in handleStreamMessage(), use x-html for output display.

**Tech Stack:** ansi_up (ESM from esm.sh), Alpine.js x-html directive

---

### Task 1: Update commands.js to import ansi_up and convert output

**Files:**
- Modify: `xkeen-go/web/static/js/components/commands.js`

**Step 1: Add ansi_up import**

Add at the top of commands.js (line 3, after api.js import):

```javascript
import { postStream } from '../services/api.js';
import ansi_up from 'https://esm.sh/ansi_up@6.0.2';
```

**Step 2: Update handleStreamMessage to convert ANSI to HTML**

Modify the `handleStreamMessage` function (around line 86-98). Replace:

```javascript
handleStreamMessage(msg) {
    if (msg.type === 'output') {
        this.$store.app.modal.output += msg.text + '\n';
        this.scrollToBottom();
    } else if (msg.type === 'error') {
        this.$store.app.modal.error += (this.$store.app.modal.error ? '\n' : '') + msg.text;
        this.scrollToBottom();
    } else if (msg.type === 'complete') {
        this.commandComplete = true;
        if (!msg.success && !this.$store.app.modal.error) {
            this.$store.app.modal.error = `Command failed with exit code ${msg.exitCode}`;
        }
    }
}
```

With:

```javascript
handleStreamMessage(msg) {
    if (msg.type === 'output') {
        // Convert ANSI escape codes to HTML
        const html = ansi_up.ansi_to_html(msg.text, { use_classes: false });
        this.$store.app.modal.output += html + '\n';
        this.scrollToBottom();
    } else if (msg.type === 'error') {
        // Also convert ANSI in error output
        const html = ansi_up.ansi_to_html(msg.text, { use_classes: false });
        this.$store.app.modal.error += (this.$store.app.modal.error ? '\n' : '') + html;
        this.scrollToBottom();
    } else if (msg.type === 'complete') {
        this.commandComplete = true;
        if (!msg.success && !this.$store.app.modal.error) {
            this.$store.app.modal.error = `Command failed with exit code ${msg.exitCode}`;
        }
    }
}
```

**Step 3: Verify changes**

Run the app and check browser console has no import errors.

---

### Task 2: Update index.html modal to use x-html

**Files:**
- Modify: `xkeen-go/web/index.html:220`

**Step 1: Change x-text to x-html for modal output**

Find line 220:
```html
<pre id="modal-output" class="modal-output" x-text="$store.app.modal.output"></pre>
```

Replace with:
```html
<pre id="modal-output" class="modal-output" x-html="$store.app.modal.output"></pre>
```

**Step 2: Change x-text to x-html for modal error**

Find line 219:
```html
<pre x-show="$store.app.modal.error" class="modal-error" x-text="$store.app.modal.error"></pre>
```

Replace with:
```html
<pre x-show="$store.app.modal.error" class="modal-error" x-html="$store.app.modal.error"></pre>
```

---

### Task 3: Test and commit

**Step 1: Test locally**

1. Build and run the app: `make run` (from xkeen-go directory)
2. Navigate to Commands tab
3. Execute `status` command
4. Verify colored text displays correctly (e.g., "остановлен" should be red)
5. Verify no raw ANSI codes like `[91m` are visible

**Step 2: Commit changes**

```bash
git add xkeen-go/web/static/js/components/commands.js xkeen-go/web/index.html
git commit -m "feat(web): render ANSI colors in command output

Use ansi_up library to convert ANSI escape codes to HTML spans.
Command output now displays with proper colors instead of raw codes.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Summary

| Task | Files Changed | Description |
|------|---------------|-------------|
| 1 | commands.js | Import ansi_up, convert ANSI to HTML |
| 2 | index.html | Use x-html for output display |
| 3 | - | Test and commit |

**No new files** - ansi_up is loaded from CDN as ES module (~5KB).
