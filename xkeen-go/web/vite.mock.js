import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

/**
 * Vite config with mock API — no Go backend needed.
 *
 * Usage:
 *   NODE_ENV=development npm run dev        # normal dev (proxies to Go backend)
 *   npm run dev:mock                         # dev with mock API
 *   vite --config vite.mock.js               # same as dev:mock
 */
export default defineConfig({
    plugins: [vue(), mockApiPlugin()],
    server: {
        port: 5173,
    },
    build: {
        outDir: 'static/dist',
        emptyOutDir: true,
        minify: true,
        sourcemap: false,
        target: 'es2020',
        rollupOptions: {
            input: 'src/main.js',
            output: {
                format: 'es',
                entryFileNames: 'bundle.js',
                inlineDynamicImports: true,
            },
        },
    },
});

// ─── Mock API Plugin ────────────────────────────────────────────────────────

function mockApiPlugin() {
    return {
        name: 'mock-api',

        // Intercept API requests before they hit the network
        configureServer(server) {
            server.middlewares.use(async (req, res, next) => {
                // Only intercept /api/* and /ws/* routes
                if (!req.url?.startsWith('/api') && !req.url?.startsWith('/ws')) {
                    return next();
                }

                const url = new URL(req.url, `http://localhost:${server.config.server.port}`);
                const path = url.pathname;
                const method = req.method;

                // ── CORS preflight ──
                if (method === 'OPTIONS') {
                    res.writeHead(204, {
                        'Access-Control-Allow-Origin': '*',
                        'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, PATCH, OPTIONS',
                        'Access-Control-Allow-Headers': 'Content-Type, X-CSRF-Token, Authorization',
                    });
                    return res.end();
                }

                // ── Handle endpoint ──
                const handler = apiHandlers[path]?.[method?.toUpperCase()];
                if (handler) {
                    try {
                        const body = await readBody(req);
                        const result = await handler(method, path, url, body, req);
                        send(res, result.status, result.body, result.extraHeaders);
                    } catch (err) {
                        send(res, 500, { error: err.message });
                    }
                    return;
                }

                // No handler — fall through to next middleware (will 404)
                next();
            });

            // WebSocket proxy for /ws/logs
            server.httpServer?.on('upgrade', (req, socket, head) => {
                const url = new URL(req.url ?? '', `http://localhost:${server.config.server.port}`);
                if ((url.pathname ?? '').startsWith('/ws')) {
                    const handler = wsHandlers[url.pathname];
                    if (handler) {
                        handler(req, socket, head, server);
                        return;
                    }
                }
                // Let default WebSocket handling continue
            });
        },
    };
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function readBody(req) {
    return new Promise((resolve, reject) => {
        let data = '';
        req.on('data', (chunk) => (data += chunk));
        req.on('end', () => {
            try {
                resolve(data ? JSON.parse(data) : null);
            } catch {
                resolve(null);
            }
        });
        req.on('error', reject);
    });
}

function send(res, status, body, extraHeaders = {}) {
    const headers = {
        'Content-Type': 'application/json',
        'Access-Control-Allow-Origin': '*',
        ...extraHeaders,
    };
    res.writeHead(status, headers);
    res.end(typeof body === 'string' ? body : JSON.stringify(body));
}

// ─── Mock Data Generators ───────────────────────────────────────────────────

let _csrfToken = 'mock-csrf-token-' + Math.random().toString(36).slice(2, 10);
let _loggedOut = true;
let _updateProgress = 0;

const apiHandlers = {
    // ── Auth ────────────────────────────────────────────────────────────────
    '/api/login': {
        POST: () => ({
            status: 200,
            body: { token: 'mock-session-' + Math.random().toString(36).slice(2), csrf_token: _csrfToken },
        }),
    },
    '/api/logout': { POST: () => ({ status: 200, body: {} }) },
    '/api/session': {
        GET: () => ({ status: _loggedOut ? 401 : 200, body: _loggedOut ? null : { authenticated: true } }),
    },

    // ── Service ─────────────────────────────────────────────────────────────
    '/api/service/status': {
        GET: () => ({
            status: 200,
            body: {
                status: 'running', // or 'stopped', 'starting', 'error'
                pid: 1234,
                uptime: '2d 3h 45m',
                version: '2.0.15',
                config_path: '/opt/etc/xray/config.json',
            },
        }),
    },
    '/api/service/start': { POST: () => ({ status: 200, body: { message: 'Service started', status: 'starting' } }) },
    '/api/service/stop': { POST: () => ({ status: 200, body: { message: 'Service stopped', status: 'stopped' } }) },
    '/api/service/restart': { POST: () => ({ status: 200, body: { message: 'Service restarted', status: 'starting' } }) },

    // ── Config ──────────────────────────────────────────────────────────────
    '/api/config/list': {
        GET: () => ({
            status: 200,
            body: [
                { path: '/opt/etc/xray/config.json', name: 'config.json', size: 1234, modified: '2026-06-01T10:00:00Z', type: 'json' },
                { path: '/opt/etc/xray/config.backup.json', name: 'config.backup.json', size: 1230, modified: '2026-05-31T08:00:00Z', type: 'json' },
                { path: '/opt/etc/xray/rulesets', name: 'rulesets', size: 0, modified: '2026-06-01T09:00:00Z', type: 'dir' },
            ],
        }),
    },
    '/api/config/read': {
        GET: (_m, path) => {
            const url = new URL(path, 'http://x');
            const filePath = url.searchParams.get('file');
            return {
                status: 200,
                body: {
                    content: JSON.stringify({
                        inbounds: [{ port: 10808, protocol: 'socks' }],
                        outbounds: [{ tag: 'proxy', server: 'example.com' }],
                    }, null, 2),
                    path: filePath || '/opt/etc/xray/config.json',
                },
            };
        },
    },
    '/api/config/save': {
        POST: (_m, _p, _url, body) => ({
            status: 200,
            body: {
                message: 'Config saved',
                backup_path: body?.file ? body.file + '.backup' : null,
            },
        }),
    },

    // ── Logs ────────────────────────────────────────────────────────────────
    '/api/logs': {
        GET: () => ({
            status: 200,
            body: [
                { time: '2026-06-01T10:00:00Z', level: 'INFO', message: 'Service started successfully' },
                { time: '2026-06-01T10:05:00Z', level: 'WARN', message: 'High memory usage detected' },
                { time: '2026-06-01T10:10:00Z', level: 'ERROR', message: 'Connection timeout to upstream' },
                { time: '2026-06-01T10:15:00Z', level: 'INFO', message: 'Rule update completed' },
            ],
        }),
    },

    // ── Metrics ─────────────────────────────────────────────────────────────
    '/api/metrics': {
        GET: () => ({
            status: 200,
            body: {
                cpu_percent: 12.5,
                memory_percent: 34.2,
                connections_active: 45,
                connections_total: 1230,
                bytes_sent: 1048576,
                bytes_received: 2097152,
                uptime_seconds: 189300,
            },
        }),
    },

    // ── Subscription ────────────────────────────────────────────────────────
    '/api/subscription': {
        GET: () => ({
            status: 200,
            body: [
                {
                    id: 'sub-1',
                    name: 'Main subscription',
                    url: 'https://example.com/sub/abc123',
                    profile_count: 5,
                    last_fetched: '2026-06-01T09:00:00Z',
                    next_run: '2026-06-01T12:00:00Z',
                    status: 'active',
                },
                {
                    id: 'sub-2',
                    name: 'Backup subscription',
                    url: 'https://example.com/sub/def456',
                    profile_count: 3,
                    last_fetched: '2026-05-31T20:00:00Z',
                    next_run: '2026-06-01T20:00:00Z',
                    status: 'paused',
                },
            ],
        }),
        POST: () => ({ status: 201, body: { id: 'sub-' + Date.now(), message: 'Subscription created' } }),
    },
    '/api/subscription/fetch': {
        POST: () => ({ status: 200, body: { message: 'Fetch started', fetched_profiles: 12 } }),
    },

    // ── Settings ────────────────────────────────────────────────────────────
    '/api/settings': {
        GET: () => ({
            status: 200,
            body: {
                port: 8089,
                log_level: 'info',
                session_timeout_hours: 24,
                max_login_attempts: 5,
                lockout_duration_minutes: 5,
                cors_enabled: false,
                allowed_origins: [],
            },
        }),
        PUT: () => ({ status: 200, body: { message: 'Settings saved' } }),
    },

    // ── Update ──────────────────────────────────────────────────────────────
    '/api/update/check': {
        GET: () => ({
            status: 200,
            body: {
                current_version: '1.0.0',
                latest_version: '1.0.1',
                update_available: true,
                release_url: 'https://github.com/fan92rus/xkeen-ui/releases/tag/v1.0.1',
            },
        }),
    },
    '/api/update/start': {
        POST: () => ({ status: 200, body: { message: 'Update started' } }),
    },

    // ── Commands ────────────────────────────────────────────────────────────
    '/api/commands/execute': {
        POST: (_m, _p, _url, body) => ({
            status: 200,
            body: {
                exit_code: 0,
                stdout: 'Command executed successfully',
                stderr: '',
            },
        }),
    },

    // ── Interactive ─────────────────────────────────────────────────────────
    '/api/interactive/exec': {
        POST: (_m, _p, _url, body) => ({
            status: 200,
            body: {
                output: `$ ${body?.command || 'ls'}\nresult: success`,
                exit_code: 0,
            },
        }),
    },

    // ── Health ──────────────────────────────────────────────────────────────
    '/health': {
        GET: () => ({ status: 200, body: { status: 'ok' } }),
    },
};

// ─── WebSocket Mock (/ws/logs) ──────────────────────────────────────────────

const wsHandlers = {
    '/ws/logs': (req, socket, head, server) => {
        // Send mock log lines periodically
        const mockMessages = [
            { time: new Date().toISOString(), level: 'INFO', message: 'Mock: Service running' },
            { time: new Date().toISOString(), level: 'WARN', message: 'Mock: High CPU usage' },
            { time: new Date().toISOString(), level: 'ERROR', message: 'Mock: Connection failed' },
            { time: new Date().toISOString(), level: 'INFO', message: 'Mock: Rule update done' },
        ];

        let idx = 0;
        const timer = setInterval(() => {
            const msg = mockMessages[idx % mockMessages.length];
            try {
                socket.write(`data: ${JSON.stringify(msg)}\n\n`);
            } catch {
                clearInterval(timer);
            }
            idx++;
        }, 3000);

        // Send initial handshake
        socket.write('data: {"type":"connected","message":"Mock logs streaming"}\n\n');

        socket.on('close', () => clearInterval(timer));
        socket.on('error', () => clearInterval(timer));

        // Upgrade the connection
        server.httpServer?.emit('upgrade', req, socket, head);
    },
};
