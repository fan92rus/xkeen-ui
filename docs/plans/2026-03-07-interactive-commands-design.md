# Interactive Commands Design

## Problem

Some XKEEN commands require user input during execution (e.g., `uk` command asks for version). Currently, there's no way to provide this input from the UI.

## Solution

Add WebSocket-based interactive command execution that supports bidirectional communication between the running process and the UI.

## Architecture

### WebSocket Endpoint

`/ws/xkeen/interactive` - new WebSocket endpoint for interactive command execution.

### Protocol

**Server → Client messages:**
```json
{"type": "output", "text": "Enter version: "}
{"type": "complete", "success": true, "exitCode": 0}
{"type": "error", "text": "command failed"}
```

**Client → Server messages:**
```json
{"type": "start", "command": "uk"}
{"type": "input", "text": "2.3.1\n"}
{"type": "signal", "signal": "SIGTERM"}
```

### Key Decisions

- One WebSocket session = one command (simpler state management)
- Server closes connection after `complete` message
- User input sent as-is (with `\n` for Enter)
- Input field always visible in UI (no prompt detection needed)

## Backend

### New File: `internal/handlers/interactive.go`

```go
type InteractiveHandler struct {
    commands  map[string]CommandConfig  // reuse from commands.go
    upgrader  websocket.Upgrader
}

func (h *InteractiveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request)
```

### Handler Logic

1. Upgrade HTTP → WebSocket (with session auth check)
2. Wait for `{"type": "start", "command": "uk"}` message
3. Start command via `exec.Command` with `StdinPipe`, `StdoutPipe`, `StderrPipe`
4. Goroutines read stdout/stderr and send `output` messages
5. Main loop reads WS messages, writes to stdin
6. On process exit, send `complete` and close WS

### Reuse

- `CommandConfig` and command whitelist from `commands.go`
- `StreamMessage` struct (extend if needed)

### Route

`/ws/xkeen/interactive` protected by AuthMiddleware

## Frontend

### New File: `web/static/js/services/interactive.js`

```javascript
class InteractiveSession {
    constructor(command, onMessage, onComplete) {
        this.ws = null;
        this.command = command;
        this.onMessage = onMessage;
        this.onComplete = onComplete;
    }

    connect() {
        this.ws = new WebSocket(`${wsBase}/ws/xkeen/interactive`);
        this.ws.onopen = () => this.ws.send(JSON.stringify({
            type: 'start', command: this.command
        }));
        this.ws.onmessage = (e) => this.handleMessage(JSON.parse(e.data));
    }

    send(text) {
        this.ws.send(JSON.stringify({ type: 'input', text }));
    }

    handleMessage(msg) {
        if (msg.type === 'complete') {
            this.ws.close();
            this.onComplete(msg);
        } else {
            this.onMessage(msg);
        }
    }
}
```

### Changes to `commands.js`

- Modal output area stays the same
- Add input field below output (always visible)
- Add Send button (or Enter key)
- On "Run" click, open WebSocket instead of fetch

### UI Layout

```
┌─────────────────────────────────────┐
│ Output:                             │
│ [stdout/stderr text...]             │
│                                     │
├─────────────────────────────────────┤
│ Input: [________________] [Send]    │
└─────────────────────────────────────┘
```

## Files to Create/Modify

### Create
- `internal/handlers/interactive.go` - WebSocket handler
- `web/static/js/services/interactive.js` - WebSocket client

### Modify
- `internal/server/server.go` - add route
- `web/static/js/components/commands.js` - use WebSocket for execution
- `web/index.html` - add input field to modal
