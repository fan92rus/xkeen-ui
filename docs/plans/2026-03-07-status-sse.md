# SSE Status Streaming Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace HTTP polling with Server-Sent Events for real-time XKeen status updates every 5 seconds with instant checks on start/stop.

**Architecture:** SSE endpoint `/api/xkeen/status/stream` streams status to connected clients. Backend runs status check loop with 5s interval and supports instant trigger after service commands.

**Tech Stack:** Go (gorilla/mux), Server-Sent Events, JavaScript EventSource API

---

## Task 1: Add SSE Status Handler

**Files:**
- Modify: `xkeen-go/internal/handlers/service.go`
- Test: `xkeen-go/internal/handlers/service_test.go` (create if needed)

**Step 1: Add SSE status stream handler**

Add to `service.go` after `GetStatus` function:

```go
// StatusStream handles SSE connections for real-time status updates.
// GET /api/xkeen/status/stream
func (h *ServiceHandler) StatusStream(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial status immediately
	h.sendStatusEvent(w, flusher)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := h.sendStatusEvent(w, flusher); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		case <-h.statusTrigger:
			// Instant check triggered by start/stop/restart
			if err := h.sendStatusEvent(w, flusher); err != nil {
				return
			}
		}
	}
}

// sendStatusEvent sends a single status event to the SSE client
func (h *ServiceHandler) sendStatusEvent(w http.ResponseWriter, flusher http.Flusher) error {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	output, err := h.executeCommandWithTimeout(ctx, "status")

	notRunning := strings.Contains(output, "is not running") ||
		strings.Contains(output, "не запущен")

	isRunning := err == nil && !notRunning &&
		(strings.Contains(output, "is running") ||
			strings.Contains(output, "running (PID:") ||
			strings.Contains(output, "active (running)") ||
			strings.Contains(output, "запущен"))

	status := ServiceStatus{
		LastCheck: time.Now(),
		Running:   isRunning,
	}
	if isRunning {
		status.Uptime = "active"
	}

	data, _ := json.Marshal(status)
	fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
	flusher.Flush()
	return nil
}
```

**Step 2: Add statusTrigger channel to ServiceHandler**

Modify `ServiceHandler` struct and constructors:

```go
// ServiceHandler handles xkeen service operations.
type ServiceHandler struct {
	mu              sync.RWMutex
	allowedCommands map[string]string
	executor        CommandExecutor
	statusTrigger   chan struct{}
}

// NewServiceHandler creates a new ServiceHandler.
func NewServiceHandler() *ServiceHandler {
	return &ServiceHandler{
		allowedCommands: map[string]string{
			"start":   "xkeen -start",
			"stop":    "xkeen -stop",
			"restart": "xkeen -restart",
			"status":  "xkeen -status",
		},
		executor:      &realExecutor{},
		statusTrigger: make(chan struct{}, 1),
	}
}

// NewServiceHandlerWithExecutor creates a ServiceHandler with a custom executor (for testing).
func NewServiceHandlerWithExecutor(executor CommandExecutor) *ServiceHandler {
	return &ServiceHandler{
		allowedCommands: map[string]string{
			"start":   "xkeen -start",
			"stop":    "xkeen -stop",
			"restart": "xkeen -restart",
			"status":  "xkeen -status",
		},
		executor:      executor,
		statusTrigger: make(chan struct{}, 1),
	}
}

// TriggerStatusCheck signals all SSE clients to receive an immediate status update.
func (h *ServiceHandler) TriggerStatusCheck() {
	select {
	case h.statusTrigger <- struct{}{}:
	default:
		// Channel full, status check already pending
	}
}
```

**Step 3: Commit backend changes**

Run:
```bash
cd xkeen-go && git add internal/handlers/service.go && git commit -m "feat: add SSE status stream handler with instant trigger"
```

---

## Task 2: Integrate Trigger into Start/Stop/Restart

**Files:**
- Modify: `xkeen-go/internal/handlers/service.go`

**Step 1: Add TriggerStatusCheck to Start handler**

Modify `Start` function to trigger status check after command:

```go
// Start starts the xkeen service.
// POST /api/xkeen/start
func (h *ServiceHandler) Start(w http.ResponseWriter, r *http.Request) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), StartStopTimeout)
		defer cancel()

		output, err := h.executeCommandWithTimeout(ctx, "start")
		if err != nil {
			log.Printf("Start failed: %v, output: %s", err, output)
		} else {
			log.Printf("Start completed: %s", output)
		}

		// Wait a moment for service to start, then trigger status check
		time.Sleep(1 * time.Second)
		h.TriggerStatusCheck()
	}()

	h.respondJSON(w, http.StatusOK, ServiceResponse{
		Success: true,
		Message: "Start initiated",
	})
}
```

**Step 2: Add TriggerStatusCheck to Stop handler**

Modify `Stop` function:

```go
// Stop stops the xkeen service.
// POST /api/xkeen/stop
func (h *ServiceHandler) Stop(w http.ResponseWriter, r *http.Request) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), StartStopTimeout)
		defer cancel()

		output, err := h.executeCommandWithTimeout(ctx, "stop")
		if err != nil {
			log.Printf("Stop failed: %v, output: %s", err, output)
		} else {
			log.Printf("Stop completed: %s", output)
		}

		// Wait a moment for service to stop, then trigger status check
		time.Sleep(1 * time.Second)
		h.TriggerStatusCheck()
	}()

	h.respondJSON(w, http.StatusOK, ServiceResponse{
		Success: true,
		Message: "Stop initiated",
	})
}
```

**Step 3: Add TriggerStatusCheck to Restart handler**

Modify `Restart` function:

```go
// Restart restarts the xkeen service.
// POST /api/xkeen/restart
func (h *ServiceHandler) Restart(w http.ResponseWriter, r *http.Request) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), RestartTimeout)
		defer cancel()

		output, err := h.executeCommandWithTimeout(ctx, "restart")
		if err != nil {
			log.Printf("Restart failed: %v, output: %s", err, output)
		} else {
			log.Printf("Restart completed: %s", output)
		}

		// Wait a moment for service to restart, then trigger status check
		time.Sleep(2 * time.Second)
		h.TriggerStatusCheck()
	}()

	h.respondJSON(w, http.StatusOK, ServiceResponse{
		Success: true,
		Message: "Restart initiated",
	})
}
```

**Step 4: Commit**

Run:
```bash
cd xkeen-go && git add internal/handlers/service.go && git commit -m "feat: trigger instant status check after start/stop/restart"
```

---

## Task 3: Register SSE Route

**Files:**
- Modify: `xkeen-go/internal/handlers/service.go`
- Modify: `xkeen-go/internal/server/server.go`

**Step 1: Add route registration in service.go**

Add to `RegisterServiceRoutes` function:

```go
// RegisterServiceRoutes registers service-related routes.
func RegisterServiceRoutes(r *mux.Router, handler *ServiceHandler) {
	r.HandleFunc("/xkeen/status", handler.GetStatus).Methods("GET")
	r.HandleFunc("/xkeen/status/stream", handler.StatusStream).Methods("GET") // SSE endpoint
	r.HandleFunc("/xkeen/start", handler.Start).Methods("POST")
	r.HandleFunc("/xkeen/stop", handler.Stop).Methods("POST")
	r.HandleFunc("/xkeen/restart", handler.Restart).Methods("POST")
}
```

**Step 2: Verify route is registered in server.go**

Check that `RegisterServiceRoutes` is called. No changes needed if already present.

**Step 3: Commit**

Run:
```bash
cd xkeen-go && git add internal/handlers/service.go && git commit -m "feat: register SSE status stream route"
```

---

## Task 4: Create Frontend SSE Service

**Files:**
- Create: `xkeen-go/web/static/js/services/status.js`

**Step 1: Create status.js SSE client**

```javascript
// services/status.js - SSE status streaming

let eventSource = null;

/**
 * Connect to SSE status stream
 * @param {function} onStatus - Callback receiving 'running' | 'stopped' | 'unknown'
 * @returns {function} Disconnect function
 */
export function connectStatusStream(onStatus) {
    if (eventSource) {
        eventSource.close();
    }

    eventSource = new EventSource('/api/xkeen/status/stream');

    eventSource.addEventListener('status', (e) => {
        try {
            const data = JSON.parse(e.data);
            onStatus(data.running ? 'running' : 'stopped');
        } catch (err) {
            console.error('Failed to parse status event:', err);
            onStatus('unknown');
        }
    });

    eventSource.onerror = (err) => {
        console.error('SSE error:', err);
        // Browser will auto-reconnect
    };

    return () => {
        if (eventSource) {
            eventSource.close();
            eventSource = null;
        }
    };
}

/**
 * Disconnect SSE status stream
 */
export function disconnectStatusStream() {
    if (eventSource) {
        eventSource.close();
        eventSource = null;
    }
}
```

**Step 2: Commit**

Run:
```bash
cd xkeen-go && git add web/static/js/services/status.js && git commit -m "feat: add SSE status stream client service"
```

---

## Task 5: Integrate SSE into Store

**Files:**
- Modify: `xkeen-go/web/static/js/store.js`

**Step 1: Add import for status service**

Add at top of file with other imports:

```javascript
import * as statusService from './services/status.js';
```

**Step 2: Update init() to connect SSE**

Modify `init()` function:

```javascript
// Init
init() {
    this.loadFiles();
    this.loadXraySettings();
    this.checkUpdate();
    // Connect to SSE status stream
    statusService.connectStatusStream((status) => {
        this.serviceStatus = status;
    });
}
```

**Step 3: Remove fetchServiceStatus calls from startService/stopService**

Modify `startService()`:

```javascript
async startService() {
    try {
        await xkeenService.start();
        this.showToast('Service starting...', 'success');
        // Status will be updated via SSE
    } catch (err) {
        this.showToast('Failed to start service', 'error');
    }
},
```

Modify `stopService()`:

```javascript
async stopService() {
    try {
        await xkeenService.stop();
        this.showToast('Service stopping...', 'success');
        // Status will be updated via SSE
    } catch (err) {
        this.showToast('Failed to stop service', 'error');
    }
},
```

**Step 4: Keep fetchServiceStatus for backward compatibility but mark as optional**

No changes needed - it can stay for manual refresh if desired.

**Step 5: Commit**

Run:
```bash
cd xkeen-go && git add web/static/js/store.js && git commit -m "feat: integrate SSE status stream into app store"
```

---

## Task 6: Remove Unused getStatus from xkeen.js

**Files:**
- Modify: `xkeen-go/web/static/js/services/xkeen.js`

**Step 1: Remove getStatus function**

Delete the `getStatus` function:

```javascript
// Remove this:
export async function getStatus() {
    const data = await get('/api/xkeen/status');
    if (data.status && data.status.running !== undefined) {
        return data.status.running ? 'running' : 'stopped';
    }
    return 'unknown';
}
```

**Step 2: Commit**

Run:
```bash
cd xkeen-go && git add web/static/js/services/xkeen.js && git commit -m "refactor: remove unused getStatus (replaced by SSE)"
```

---

## Task 7: Build and Test

**Step 1: Build the application**

Run:
```bash
cd xkeen-go && make build
```

Expected: Build succeeds without errors

**Step 2: Manual test**

1. Run the application: `./xkeen-go`
2. Open browser to the UI
3. Open DevTools Network tab
4. Verify SSE connection to `/api/xkeen/status/stream`
5. Click Start/Stop and verify instant status update
6. Wait 5+ seconds and verify periodic updates

**Step 3: Final commit (if any fixes needed)**

Run:
```bash
cd xkeen-go && git add -A && git commit -m "fix: any fixes from testing"
```

---

## Summary

| Task | Description |
|------|-------------|
| 1 | Add SSE status stream handler |
| 2 | Integrate trigger into start/stop/restart |
| 3 | Register SSE route |
| 4 | Create frontend SSE service |
| 5 | Integrate SSE into store |
| 6 | Remove unused getStatus |
| 7 | Build and test |
