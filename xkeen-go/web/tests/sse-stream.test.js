// tests/sse-stream.test.js — Tests for the shared SSE stream reader
import { describe, it, expect, vi } from 'vitest';
import { readSSEStream } from '../src/utils/sse-stream.js';

// Helper: build a ReadableStream from an array of chunks
function makeResponse(chunks) {
    const encoder = new TextEncoder();
    const stream = new ReadableStream({
        start(controller) {
            for (const chunk of chunks) {
                controller.enqueue(encoder.encode(chunk));
            }
            controller.close();
        }
    });
    return { body: stream };
}

describe('readSSEStream', () => {
    it('dispatches progress events', async () => {
        const onProgress = vi.fn();
        const res = makeResponse([
            'event: progress\ndata: {"percent": 50, "status": "halfway"}\n\n',
        ]);
        // Stream ends without complete — readSSEStream resolves with void
        await readSSEStream(res, { onProgress });
        expect(onProgress).toHaveBeenCalledWith({ percent: 50, status: 'halfway' });
    });

    it('resolves with complete event data', async () => {
        const onComplete = vi.fn();
        const res = makeResponse([
            'event: complete\ndata: {"percent": 100, "status": "done"}\n\n',
        ]);
        const result = await readSSEStream(res, { onComplete });
        expect(onComplete).toHaveBeenCalledWith({ percent: 100, status: 'done' });
        expect(result).toEqual({ percent: 100, status: 'done' });
    });

    it('rejects on error event', async () => {
        const onError = vi.fn();
        const res = makeResponse([
            'event: error\ndata: {"error": "disk full"}\n\n',
        ]);
        await expect(readSSEStream(res, { onError })).rejects.toThrow('disk full');
        expect(onError).toHaveBeenCalledWith({ error: 'disk full' });
    });

    it('skips malformed JSON lines without hanging', async () => {
        const onProgress = vi.fn();
        const onComplete = vi.fn();
        const res = makeResponse([
            'event: progress\ndata: {bad json}\n\n',
            'event: progress\ndata: {"percent": 80}\n\n',
            'event: complete\ndata: {"status": "ok"}\n\n',
        ]);
        const result = await readSSEStream(res, { onProgress, onComplete });
        // Malformed line skipped, valid ones processed
        expect(onProgress).toHaveBeenCalledTimes(1);
        expect(onProgress).toHaveBeenCalledWith({ percent: 80 });
        expect(result).toEqual({ status: 'ok' });
    });

    it('resolves when stream ends without complete event', async () => {
        const onComplete = vi.fn();
        const res = makeResponse([
            'event: progress\ndata: {"percent": 50}\n\n',
        ]);
        const result = await readSSEStream(res, { onComplete });
        expect(result).toBeUndefined();
        // onComplete called synthetically since stream ended
        expect(onComplete).toHaveBeenCalledWith({ percent: 100, status: 'complete' });
    });

    it('handles multi-line data chunks split across boundaries', async () => {
        const onProgress = vi.fn();
        const res = makeResponse([
            'event: progr',         // split mid-event-name
            'ess\ndata: {"percent"',  // split mid-json
            ': 10}\n\nevent: complete\ndata: {"status":"ok"}\n\n',
        ]);
        const result = await readSSEStream(res, { onProgress });
        expect(onProgress).toHaveBeenCalledWith({ percent: 10 });
        expect(result).toEqual({ status: 'ok' });
    });

    it('handles error event with missing error field', async () => {
        const res = makeResponse([
            'event: error\ndata: {}\n\n',
        ]);
        await expect(readSSEStream(res)).rejects.toThrow('Unknown error');
    });

    it('handles empty stream gracefully', async () => {
        const res = makeResponse([]);
        const result = await readSSEStream(res);
        expect(result).toBeUndefined();
    });
});
