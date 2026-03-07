# Streaming Commands Design

**Date:** 2026-03-06
**Status:** Approved
**Problem:** Long-running commands hang in "Executing..." state and timeout after ~3 minutes without showing output

## Problem Analysis

### Root Cause
- HTTP server `WriteTimeout = 15 seconds` in `server.go:132`
- `/api/xkeen/command` endpoint is synchronous and waits for command completion
- Commands can take up to 120 seconds (update operations)
- Server terminates connection before command completes
- Frontend has no explicit timeout on fetch requests

### Affected Commands
All commands called via `/api/xkeen/command` (Commands tab):
- `start`, `stop` - 30s timeout
- `restart` - 45s timeout
- `kb`, `kbr` - 60s timeout
- `uk`, `ug`, `ux`, `um` - 120s timeout

## Solution: NDJSON Streaming

### Overview
Replace synchronous command execution with streaming response using NDJSON (Newline-Delimited JSON) format. Output appears in real-time in the modal dialog.

### Backend Changes

#### 1. server.go
- Increase `WriteTimeout` from 15s to 300s (5 minutes)

#### 2. commands.go - ExecuteCommand handler
- Set response headers:
  - `Content-Type: application/x-ndjson`
  - `Transfer-Encoding: chunked`
- Use `exec.Command` with `StdoutPipe` and `StderrPipe` for line-by-line reading
- Flush after each message
- Message format:
  ```json
  {"type":"output","text":"line of stdout"}
  {"type":"error","text":"line of stderr"}
  {"type":"complete","success":true,"exitCode":0}
  ```

#### 3. Error handling
- Timeout: send error message + complete with success=false
- Unknown command: immediate error response
- Command not found: error + complete with exitCode=127

### Frontend Changes

#### commands.js - doExecute method
- Open modal immediately
- Use `fetch` with `ReadableStream` reader
- Parse NDJSON line by line
- Update `modalOutput` and `modalError` in real-time
- Handle stream completion and errors

#### UX improvements
- Auto-scroll to bottom on new output
- Keep "Executing..." indicator until `type: complete`
- Handle modal close with `reader.cancel()`

## Message Types

| Type | Fields | Description |
|------|--------|-------------|
| `output` | `text` | stdout line |
| `error` | `text` | stderr line or error message |
| `complete` | `success`, `exitCode` | command finished |

## Configuration

- `WriteTimeout`: 300 seconds (covers max command timeout + buffer)
- Command timeouts remain unchanged (10-120s depending on command)

## Trade-offs

### Chosen: NDJSON over SSE
- Simpler implementation
- Works natively with POST requests
- Easy line-by-line parsing

### Chosen: Single timeout increase over separate server
- Simpler architecture
- Sufficient for internal tool
- Less configuration overhead

## Files to Modify

1. `xkeen-go/internal/server/server.go` - WriteTimeout
2. `xkeen-go/internal/handlers/commands.go` - Streaming execution
3. `xkeen-go/web/static/js/components/commands.js` - Streaming fetch

## Testing Plan

1. Test short commands (< 1s) - should work as before
2. Test long commands (> 15s) - should stream output
3. Test timeout scenarios - should show error message
4. Test network interruption - should show error
5. Test modal close during execution - should cancel stream
