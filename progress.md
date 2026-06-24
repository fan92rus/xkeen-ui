# XKEEN-UI Progress

## Completed

### 2026-06-24: Concurrency fixes — logs + metrics WebSocket handlers

### 2026-06-24: XSS hardening — safe ANSI/JSON rendering, dead code removal

**Files changed:** `web/src/App.vue`, `web/src/components/CommandsTab.vue`, `web/src/components/SubscriptionsTab.vue`, `web/static/js/login.js`, `web/static/dist/bundle.js`

**New files:** `web/src/utils/escape.js`, `web/src/utils/ansi-format.js`, `web/src/utils/json-format.js`, `web/tests/escape.test.js`, `web/tests/ansi-format.test.js`, `web/tests/json-format.test.js`

**Fixes:**

1. **XSS via v-html on network data** (HIGH):
   - Created `escape.js` with complete HTML escaping (`&<>"'` + U+2028/U+2029)
   - Created `ansi-format.js` with `renderAnsi()` — escape-first ANSI→HTML renderer (pro\-vably safe: all HTML chars escaped BEFORE span insertion)
   - Created `json-format.js` with `formatJson()` — safe JSON highlighting
   - CommandsTab.vue: stores raw text (not HTML) in `modal.output`/`modal.error`; removed `ansi_up` dependency
   - App.vue: renders modal output via computed `safeModalOutput`/`safeModalError` using `renderAnsi()`
   - SubscriptionsTab.vue: replaced local `_esc`/`_fmtJson` with shared `formatJson()` from utils

2. **Dead CSRF code** (LOW): `login.js` stored CSRF token in `localStorage` but api.js reads from `document.cookie` — removed both `localStorage.setItem('csrfToken', ...)` calls

**Tests added (42 new):**
- `escape.test.js`: 13 tests covering all escape chars, XSS vectors, edge cases
- `ansi-format.test.js`: 18 tests covering ANSI rendering, style reset, non-SGR stripping, XSS safety
- `json-format.test.js`: 11 tests covering JSON highlighting, HTML escaping, edge cases

**Validation:** All 92 frontend tests pass (0.66s), `npm run build` succeeds, Go build succeeds
**Commit:

**Files changed:** `internal/handlers/logs.go`, `internal/handlers/metrics.go`, `internal/handlers/logs_test.go`, `internal/handlers/metrics_test.go`

**Fixes:**

1. **Close() race** (CRITICAL): Removed `close(h.broadcast)` from `Close()` — goroutines exit via context cancellation instead. Changed `runBroadcast` from `for msg := range h.broadcast` to `select` on both `h.broadcast` and `h.ctx.Done()`/`h.doneCh`. This eliminates the theoretical race between `close(ch)` and concurrent `ch <- msg` in a select statement.

2. **Head-of-line blocking** (HIGH): Extracted `sendToClients()` method that sets `conn.SetWriteDeadline(5s)` before each write. A slow client times out instead of blocking all other clients. Dead clients are detected and removed.

**Tests added:**
- `TestLogsHandler_CloseWithActiveSenders` — 20 iterations with active file writer, verifies no panic via recover
- `TestLogsHandler_SendToClients_DeadClientRemoved` — connects WS, kills client, verifies removal
- `TestMetricsHandler_CloseWithActiveSenders` — 20 iterations with live Xray mock, verifies no panic
- `TestMetricsHandler_SendToClients_DeadClientRemoved` — same for metrics

**Validation:** All handlers tests pass (17.3s), all packages pass (71.2s)
**Commit:** `b0eebb7`

### 2026-06-24: Lifecycle fixes — interactive goroutine leak + update graceful shutdown

**Files changed:** `internal/handlers/interactive.go`, `internal/handlers/interactive_test.go`,
`internal/handlers/update.go`, `main.go`

**Fixes:**

1. **Interactive goroutine leak** (CRITICAL, Bug 1):
   PTY→WS and WS→PTY goroutines now tracked via `sync.WaitGroup`. Each goroutine
   uses `SetReadDeadline` (500ms PTY, 200ms WS) to periodically check `ctx.Done()`.
   After `cmd.Wait()`: cancel context → join goroutines with 3s timeout → send
   'complete' safely (no race on conn.WriteJSON). Prevents unbounded goroutine leak
   after command completion or shutdown.

2. **update.go os.Exit without cleanup** (CRITICAL, Bug 2):
   Replaced `os.Exit(0)` with send on package-level `UpdateShutdownCh`. Main.go
   now select-s on both OS signals (SIGINT/SIGTERM) and `UpdateShutdownCh`.
   Update-triggered shutdown uses the same graceful `srv.Stop()` path as OS signals.

3. **getLatestPrerelease pagination** (MEDIUM, Bug 3):
   Added `?per_page=100` to GitHub releases API URL (default 30). Handles dev
   prereleases beyond page 1 without full pagination support.

**Tests added:**
- `TestExecuteInteractive_HandlerReturnsWithinTimeout` — verifies handler returns
  within 30s after command completion (PTY fail or success)
- `TestExecuteInteractive_SignalMidCommand` — verifies signal path doesn't leak

**Validation:** All packages pass (handlers 32s, total 76s). go build/vet clean.
**Commit:** `bf01e27`
