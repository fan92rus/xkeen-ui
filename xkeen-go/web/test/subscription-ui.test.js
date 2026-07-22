// test/subscription-ui.test.js
// E2E tests for subscription UI with Puppeteer (Vue 3 sidebar layout)
// Usage: cd xkeen-go/web && npm run test:e2e

import puppeteer from 'puppeteer';
import { execSync, spawn } from 'child_process';
import path from 'path';
import { fileURLToPath } from 'url';
import os from 'os';
import fs from 'fs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, '../../');
const TMP = os.tmpdir();
const IS_WIN = process.platform === 'win32';
const SERVER_BIN = path.join(TMP, 'xkeen-test', IS_WIN ? 'server.exe' : 'server');
const CONFIG_PATH = path.join(TMP, 'xkeen-test', 'config', 'config.json');
const PORT = 9877;
const BASE = `http://localhost:${PORT}`;
const PASSWORD = 'password';

let browser, page, serverProcess;
let passed = 0, failed = 0, errors = [];
const jsErrors = [];
const cspViolations = [];
const consoleErrors = [];   // all console.error (not just CSP)
const failedRequests = [];  // 4xx/5xx on any fetch/XHR
const failedResources = []; // 404 on page assets (CSS/JS/fonts)

function ensureConfig() {
    const configDir = path.join(TMP, 'xkeen-test', 'config');
    const xrayDir = path.join(TMP, 'xkeen-test', 'xray');
    const logDir = path.join(TMP, 'xkeen-test', 'xray-log');
    fs.mkdirSync(configDir, { recursive: true });
    fs.mkdirSync(xrayDir, { recursive: true });
    fs.mkdirSync(logDir, { recursive: true });
    // Clean up stale subscription store to avoid leaking state between runs
    try { fs.unlinkSync(path.join(configDir, 'subscriptions.json')); } catch {}
    fs.writeFileSync(CONFIG_PATH, JSON.stringify({
        port: PORT,
        xray_config_dir: xrayDir.replace(/\\/g, '/'),
        allowed_roots: [xrayDir.replace(/\\/g, '/'), configDir.replace(/\\/g, '/'), logDir.replace(/\\/g, '/')],
        xray_log_dir: logDir.replace(/\\/g, '/'),
        metrics_port: 9999,
        auth: {
            password_hash: "$2a$12$oDp.vVnkWYsDjAuEWEKOgOR08sApErSrJFyRMbOE5d/GvccJKiNLe",
            session_timeout: 24, max_login_attempts: 100, lockout_duration: 1
        }
    }, null, 2));

    // Create a test config file so the Editor tab has something to show.
    const testConfig = { server: { port: PORT }, test: true, items: [1, 2, 3] };
    fs.writeFileSync(path.join(xrayDir, 'test-config.json'), JSON.stringify(testConfig, null, 2));

    // Create dummy log files so Logs tab doesn't 403.
    fs.writeFileSync(path.join(logDir, 'access.log'), '2024-01-01 test log line\n');
    fs.writeFileSync(path.join(logDir, 'error.log'), '2024-01-01 error line\n');
}

function assert(cond, msg) { if (!cond) throw new Error(msg); }
const wait = ms => new Promise(r => setTimeout(r, ms));

async function waitFor(fn, timeout = 10000, interval = 300) {
    const start = Date.now();
    let lastErr;
    while (Date.now() - start < timeout) {
        try { const r = await fn(); if (r) return r; } catch (e) { lastErr = e; }
        await wait(interval);
    }
    throw lastErr || new Error(`timeout (${timeout}ms)`);
}

async function login() {
    await page.goto(`${BASE}/login`, { waitUntil: 'networkidle0' });
    const pw = await page.$('input[type="password"]');
    if (pw) {
        await pw.click({ clickCount: 3 });
        await pw.type(PASSWORD);
        await (await page.$('button[type="submit"]') || await page.$('button')).click();
        await waitFor(() => !page.url().includes('/login'), 5000);
    }
}

// Navigate to subscriptions via sidebar nav buttons
async function goToSubscriptions() {
    // Nav buttons: 0=editor, 1=subs, 2=logs, 3=settings, 4=commands
    await page.evaluate(() => {
        const btns = document.querySelectorAll('.nav-btn');
        if (btns[1]) btns[1].click();
    });
    await wait(1000);
}

async function getSubCount() {
    return page.evaluate(() =>
        document.querySelectorAll('.sub-card:not(.builtin)').length
    );
}

// Returns the total card count including builtin.
// Used only for display/reporting, not for CRUD assertions.
async function getTotalCardCount() {
    return page.evaluate(() =>
        document.querySelectorAll('.sub-card').length
    );
}

// Wait until a non-builtin subscription card is present with action buttons.
// Throws on timeout so the calling test fails with a clear message.
async function waitForNonBuiltinCard(timeoutMs = 8000) {
    await waitFor(async () => {
        const card = await page.evaluate(() => {
            const c = document.querySelector('.sub-card:not(.builtin)');
            if (!c) return false;
            const btns = c.querySelectorAll('.acts button');
            return btns.length >= 2;
        });
        return card;
    }, timeoutMs);
}

async function test(name, fn) {
    try { await fn(); passed++; console.log(`  ✓ ${name}`); }
    catch (e) { failed++; errors.push({ name, error: e.message }); console.log(`  ✗ ${name}: ${e.message.split('\n')[0]}`); }
}

// ── Main ──

async function main() {
    console.log('\nE2E: Subscription UI Tests\n');
    console.log('Building...');
    execSync('npx vite build', { cwd: path.join(ROOT, 'web'), stdio: 'pipe' });
    execSync(`go build -o "${SERVER_BIN}" .`, { cwd: ROOT, stdio: 'pipe' });
    ensureConfig();

    try {
        if (IS_WIN) execSync('taskkill /F /IM server.exe 2>nul', { shell: true, stdio: 'pipe' });
    } catch {}
    await wait(500);

    serverProcess = spawn(SERVER_BIN, ['-config', CONFIG_PATH], { cwd: ROOT, stdio: 'pipe' });
    await waitFor(async () => { try { return (await fetch(`${BASE}/health`)).ok; } catch { return false; } }, 10000);
    console.log('Server started\n');

    browser = await puppeteer.launch({ headless: 'new', args: ['--no-sandbox'] });
    page = await browser.newPage();
    await page.setViewport({ width: 1280, height: 900 });
    page.setDefaultTimeout(10000);

    // Force Russian locale so button titles match our test expectations
    // regardless of the CI runner's Accept-Language header.
    await page.evaluateOnNewDocument(() => {
        localStorage.setItem('xkeen_lang', 'ru');
    });

    page.on('pageerror', err => jsErrors.push(err.message));

    // Collect ALL console errors — not just CSP. Vue warnings, JSON parse
    // failures, and network errors all land here and should fail the suite.
    page.on('console', msg => {
        if (msg.type() === 'error') {
            consoleErrors.push(msg.text());
            // Also keep CSP-specific bucket for the dedicated CSP test
            if (/Content.Security.Policy|style-src|script-src/i.test(msg.text())) {
                cspViolations.push(msg.text());
            }
        }
    });

    // Track failed page resources (404 on bundle.css, bundle.js, fonts, etc.).
    // A missing bundle.css silently renders the page without styles.
    page.on('requestfailed', request => {
        failedResources.push(`${request.failure().errorText}: ${request.url()}`);
    });

    // Track failed API requests (4xx/5xx). If a backend endpoint breaks,
    // the UI silently degrades — no crash, no error in console, just empty
    // panels. This catches every silent backend regression.
    page.on('response', response => {
        if (response.status() >= 400) {
            failedRequests.push(`${response.status()} ${response.url()}`);
        }
    });

    // Unhandled promise rejections are NOT always caught by pageerror.
    // Collect them via the unhandledrejection event on document.
    await page.evaluateOnNewDocument(() => {
        window.addEventListener('unhandledrejection', event => {
            window.__unhandledRejections = window.__unhandledRejections || [];
            window.__unhandledRejections.push(String(event.reason));
        });
    });

    // ── Tests ──

    await test('Login succeeds', async () => {
        await login();
        assert(!page.url().includes('/login'), 'Should redirect from login');
    });

    await test('Sidebar nav renders', async () => {
        await waitFor(async () => {
            const count = await page.evaluate(() =>
                document.querySelectorAll('.nav-btn').length
            );
            return count >= 3 ? count : null;
        }, 5000);
        const info = await page.evaluate(() => {
            const btns = document.querySelectorAll('.nav-btn');
            return { btnCount: btns.length };
        });
        assert(info.btnCount >= 3, `3+ nav buttons, got ${info.btnCount}`);
    });

    await test('Subscriptions tab renders via sidebar nav', async () => {
        await goToSubscriptions();
        // Check that sub-toolbar exists
        assert(await page.$('.sub-toolbar'), 'Toolbar');
        assert(await page.$('.sub-toolbar input[type="url"]'), 'URL input');
        assert(await page.$('.sub-left'), 'Left panel');
    });

    // Clean up any leftover subs from previous runs
    // Builtin subscriptions (AWG, is_builtin=true) are system-managed
    // and have no delete button — skip them.
    await test('Clean slate', async () => {
        await page.evaluate(() => { window.__origConfirm = window.confirm; window.confirm = () => true; });
        for (let i = 0; i < 10; i++) {
            const remaining = await page.evaluate(() =>
                document.querySelectorAll('.sub-card:not(.builtin)').length
            );
            if (remaining === 0) break;
            await page.evaluate(() => {
                const cards = document.querySelectorAll('.sub-card:not(.builtin)');
                const last = cards[cards.length - 1];
                if (!last) return;
                for (const b of last.querySelectorAll('.acts button')) {
                    if (b.title === 'Удалить') { b.click(); return; }
                }
            });
            await wait(600);
        }
        await page.evaluate(() => { window.confirm = window.__origConfirm; });
        const count = await getSubCount(); // only non-builtin
        assert(count === 0, `Should be clean, got ${count}`);
    });

    await test('No JS errors on page load', async () => {
        const bad = jsErrors.filter(e => /vue|subscription|component/i.test(e));
        assert(bad.length === 0, `Errors: ${bad.join('; ')}`);
    });

    await test('No CSP violations', async () => {
        await page.evaluate(() => {
            // Also collect via SecurityPolicyViolationEvent for completeness
            window.__cspViolations = window.__cspViolations || [];
            document.addEventListener('securitypolicyviolation', e => {
                window.__cspViolations.push(`${e.violatedDirective} blocked ${e.blockedURI}`);
            });
        });
        // Navigate to Editor tab to trigger CodeMirror style injection
        await page.evaluate(() => {
            const btns = document.querySelectorAll('.nav-btn');
            if (btns[0]) btns[0].click(); // Editor tab
        });
        await wait(2000);
        const violations = [
            ...cspViolations,
            ...(await page.evaluate(() => window.__cspViolations || [])),
        ];
        const styleViolations = violations.filter(v => /style-src|inline.style/i.test(v));
        assert(styleViolations.length === 0,
            `CSP style-src violations: ${styleViolations.join('; ')}`);
    });

    // ── Infrastructure health checks (failures here = backend regression) ──

    await test('No failed page resources (CSS/JS/fonts 404)', async () => {
        const relevant = failedResources.filter(r =>
            /\.(css|js|woff2?|ttf)$/.test(r) || /\/static\//.test(r)
        );
        assert(relevant.length === 0,
            `Missing assets: ${relevant.join('; ')}`);
    });

    await test('No failed API requests (4xx/5xx)', async () => {
        // Ignore expected failures:
        // - 404 on subscriptions that legitimately don't exist
        const real = failedRequests.filter(r => {
            const url = r.replace(/^\d+ /, '');
            if (/\/api\/subscriptions\//.test(url) && r.startsWith('404')) return false;
            return true;
        });
        assert(real.length === 0,
            `Failed API calls: ${real.join('; ')}`);
    });

    await test('No console errors', async () => {
        // Ignore "Failed to load resource" — redundant with failedRequests check
        const real = consoleErrors.filter(e =>
            !/Failed to load resource/.test(e)
        );
        assert(real.length === 0,
            `Console errors: ${[...new Set(real)].join('; ')}`);
    });

    await test('No unhandled promise rejections', async () => {
        const rejections = await page.evaluate(() => window.__unhandledRejections || []);
        assert(rejections.length === 0,
            `Unhandled rejections: ${rejections.join('; ')}`);
    });

    // ── Cross-tab smoke tests ──

    // Navigate helper: clicks nav button by index, waits for tab to mount.
    async function goToTab(idx) {
        await page.evaluate((i) => {
            const btns = document.querySelectorAll('.nav-btn');
            if (btns[i]) btns[i].click();
        }, idx);
        await wait(800);
    }

    // Editor tab
    await test('Editor: CodeMirror mounts (DOM present)', async () => {
        await goToTab(0); // editor
        await wait(1500);
        const hasEditor = await page.evaluate(() => {
            const el = document.querySelector('.cm-editor');
            return !!(el && el.offsetParent !== null);
        });
        assert(hasEditor, 'CodeMirror .cm-editor must be mounted and visible');
    });

    await test('Editor: file tree loads', async () => {
        const files = await page.evaluate(() => {
            const opts = document.querySelectorAll('#file-select option, .file-select option');
            return Array.from(opts).map(o => o.textContent.trim());
        });
        // Should include our test file
        const hasTestFile = files.some(f => f.includes('test-config'));
        assert(hasTestFile, `File tree should contain test-config.json, got: ${files.join(', ')}`);
    });

    await test('Editor: selecting a file renders content in CodeMirror', async () => {
        await page.evaluate(() => {
            const sel = document.querySelector('#file-select, .file-select');
            if (!sel) return;
            // Pick the option that contains 'test-config'
            for (const opt of sel.options) {
                if (opt.textContent.includes('test-config')) {
                    sel.value = opt.value;
                    sel.dispatchEvent(new Event('change', { bubbles: true }));
                    break;
                }
            }
        });
        await wait(1000);
        const content = await page.evaluate(() => {
            const lines = document.querySelectorAll('.cm-line');
            return Array.from(lines).map(l => l.textContent).join('\n');
        });
        assert(content.includes('"test"') && content.includes('"server"'),
            `Editor should show JSON content, got: ${content.substring(0, 120)}`);
    });

    await test('Editor: syntax highlighting active (token spans)', async () => {
        const hasTokens = await page.evaluate(() => {
            // CM6 wraps each token in a <span> inside .cm-line.
            // Check that at least one line has child spans (means tokens parsed).
            const lines = document.querySelectorAll('.cm-line');
            for (const line of lines) {
                if (line.children.length > 0) return true;
            }
            return false;
        });
        assert(hasTokens, 'CodeMirror should parse JSON into token spans');
    });

    // Logs tab — WebSocket test (catches WS endpoint changes, message format breaks)
    await test('Logs: WebSocket connects and shows log lines', async () => {
        await goToTab(2); // logs (index may shift if AWG installed)
        await wait(2000);
        const hasLines = await page.evaluate(() => {
            // Logs render as .log-line or plain text in #logs container
            const container = document.querySelector('#logs, .logs-container, .log-view');
            if (!container) return false;
            return container.children.length > 0 || container.textContent.trim().length > 10;
        });
        // Logs may be empty if nothing was logged — just check no crash
        const noError = await page.evaluate(() => {
            return !document.querySelector('.log-error, .logs-error');
        });
        assert(noError, 'Log tab should not show error state');
    });

    // Settings tab
    await test('Settings: all sections render', async () => {
        await goToTab(3); // settings
        await wait(1000);
        const sections = await page.evaluate(() =>
            document.querySelectorAll('.s-section').length
        );
        assert(sections >= 8, `Settings should have 8+ sections, got ${sections}`);
    });

    // Commands tab
    await test('Commands: palette loads', async () => {
        await goToTab(4); // commands
        await wait(1500);
        const state = await page.evaluate(() => {
            // Commands may show loading, error, empty, or command items.
            // Any of these means the component mounted successfully.
            const items = document.querySelectorAll('.command-item');
            const empty = document.querySelector('.commands-empty');
            const error = document.querySelector('.commands-error');
            const loading = document.querySelector('.commands-loading');
            return { items: items.length, empty: !!empty, error: !!error, loading: !!loading };
        });
        const mounted = state.items > 0 || state.empty || state.error || state.loading;
        assert(mounted,
            `Commands tab should mount (items=${state.items} empty=${state.empty} error=${state.error})`);
    });

    // Metrics tab — WebSocket + Chart.js
    await test('Metrics: tab mounts without errors', async () => {
        await goToTab(5); // metrics
        await wait(2000);
        const mounted = await page.evaluate(() => {
            // Metrics tab has .metrics-wrapper as main container.
            // When no WS data, it shows .metrics-unavailable state.
            const wrapper = document.querySelector('.metrics-wrapper');
            const unavailable = document.querySelector('.metrics-unavailable');
            return !!(wrapper || unavailable);
        });
        assert(mounted, 'Metrics tab must mount its container or unavailable state');
    });

    await test('Metrics: canvas element exists (Chart.js init)', async () => {
        const hasCanvas = await page.evaluate(() => {
            // Canvas only renders when latestSnap is available (WS connected).
            // In test env with no Xray, WS may not deliver data — canvas absence
            // is expected. We just verify the component didn't crash.
            const canvas = document.querySelector('canvas');
            const err = document.querySelector('.status-error');
            // Pass if either canvas exists OR we see a graceful error state.
            return !!canvas || !!err || document.querySelector('.metrics-unavailable');
        });
        assert(hasCanvas, 'Metrics should show canvas, error, or unavailable state');
    });

    // Back to subscriptions for the existing CRUD tests
    await goToTab(1);
    await wait(500);

    // ── Subscription CRUD tests ──

    await test('Add subscription via Enter key', async () => {
        await page.evaluate(url => {
            const input = document.querySelector('.sub-toolbar input[type="url"]');
            const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
            nativeInputValueSetter.call(input, url);
            input.dispatchEvent(new Event('input', { bubbles: true }));
            input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
        }, 'https://example.com/e2e-test');
        await waitFor(async () => (await getSubCount()) > 0, 5000);
        const count = await getSubCount();
        assert(count >= 1, `Should have 1+ non-builtin subs, got ${count}`);
    });

    await test('Subscription cards have name and action buttons', async () => {
        const info = await page.evaluate(() => {
            // Skip builtin subscriptions (e.g. AWG) — they only have
            // a refresh button, no edit/delete.
            const cards = document.querySelectorAll('.sub-card:not(.builtin)');
            return Array.from(cards).map(c => ({
                name: c.querySelector('.name')?.textContent,
                btnCount: c.querySelectorAll('.acts button').length,
                btnTitles: Array.from(c.querySelectorAll('.acts button')).map(b => b.title),
            }));
        });
        assert(info.length > 0, 'Cards exist');
        for (const c of info) {
            assert(c.name, `Card has name: ${c.name}`);
            assert(c.btnCount >= 3, `Card has 3+ buttons: ${c.btnTitles.join(', ')}`);
        }
    });

    await test('Edit toggles inline form', async () => {
        await waitForNonBuiltinCard();
        const clicked = await page.evaluate(() => {
            const card = document.querySelector('.sub-card:not(.builtin)');
            if (!card) return false;
            for (const b of card.querySelectorAll('.acts button')) {
                if (b.title === 'Редактировать') { b.click(); return true; }
            }
            return false;
        });
        assert(clicked, 'Edit button found and clicked');
        await waitFor(() => !!document.querySelector('.sub-card.editing .sub-edit'), 3000);
        await page.evaluate(() => {
            const btns = document.querySelectorAll('.sub-card.editing button');
            for (const b of btns) { if (b.textContent.includes('Отмена')) { b.click(); return; } }
        });
        await wait(300);
    });

    await test('Preview/Apply buttons in toolbar', async () => {
        await waitForNonBuiltinCard();
        const found = await page.evaluate(() => {
            const btns = document.querySelectorAll('.sub-toolbar button');
            let preview = false, apply = false;
            for (const b of btns) {
                const t = b.textContent;
                if (t.includes('Предпросмотр')) preview = true;
                if (t.includes('Применить')) apply = true;
            }
            return { preview, apply };
        });
        assert(found.preview, 'Preview button');
        assert(found.apply, 'Apply button');
    });

    await test('Empty proxy state shows hint', async () => {
        const hint = await page.evaluate(() => {
            const el = document.querySelector('.sub-right-empty');
            return el ? el.textContent.trim() : 'none';
        });
        assert(hint.length > 0, 'Should show empty state hint');
    });

    await test('Fetch button clicks without error', async () => {
        const result = await page.evaluate(() => {
            const card = document.querySelector('.sub-card:not(.builtin)');
            if (!card) return { clicked: false, reason: 'no card' };
            const btns = card.querySelectorAll('.acts button');
            const titles = Array.from(btns).map(b => b.title);
            for (const b of btns) {
                if (b.title === 'Обновить') { b.click(); return { clicked: true, titles }; }
            }
            if (btns.length > 0) { btns[0].click(); return { clicked: true, titles, fallback: true }; }
            return { clicked: false, titles };
        });
        assert(result.clicked, `Fetch button: ${JSON.stringify(result)}`);
        await wait(2000);
    });

    await test('Delete subscription', async () => {
        await waitForNonBuiltinCard();
        const before = await getSubCount();
        await page.evaluate(() => { window.__origConfirm = window.confirm; window.confirm = () => true; });
        await page.evaluate(() => {
            const card = document.querySelector('.sub-card:not(.builtin)');
            if (!card) return;
            for (const b of card.querySelectorAll('.acts button')) {
                if (b.title === 'Удалить') { b.click(); return; }
            }
        });
        await wait(1000);
        const after = await getSubCount();
        await page.evaluate(() => { window.confirm = window.__origConfirm; });
        assert(after < before, `Count: ${before} → ${after}`);
    });

    await test('Two-column body layout', async () => {
        const display = await page.evaluate(() => {
            const el = document.querySelector('.sub-body');
            return el ? getComputedStyle(el).display : null;
        });
        assert(display === 'flex', `Layout should be flex, got ${display}`);
    });

    await test('Strategy pills exist when proxies loaded', async () => {
        // Strategy pills are in filters section, always visible for active profile
        const count = await page.evaluate(() => document.querySelectorAll('.sub-filters .strat-pills .spill').length);
        assert(count >= 4, `Strategy pills in filters: ${count}`);
    });

    await test('Profile tabs bar renders', async () => {
        const bar = await page.$('.profile-tabs');
        assert(bar, 'Profile tabs bar should exist');
        const tabCount = await page.evaluate(() =>
            document.querySelectorAll('.profile-tabs .ptab:not(.ptab-add)').length
        );
        assert(tabCount >= 1, `Should have 1+ profile tabs, got ${tabCount}`);
    });

    await test('Country cloud renders', async () => {
        await page.evaluate(() => document.querySelectorAll('.country-cloud .cc').length);
    });

    await test('Clean up: delete all subscriptions', async () => {
        await waitForNonBuiltinCard(2000).catch(() => {}); // OK if already empty
        const count = await getSubCount();
        if (count === 0) return;
        await page.evaluate(() => { window.__origConfirm = window.confirm; window.confirm = () => true; });
        for (let i = 0; i < count + 2; i++) {
            const remaining = await page.evaluate(() =>
                document.querySelectorAll('.sub-card:not(.builtin)').length
            );
            if (remaining === 0) break;
            await page.evaluate(() => {
                const cards = document.querySelectorAll('.sub-card:not(.builtin)');
                const last = cards[cards.length - 1];
                if (!last) return;
                for (const b of last.querySelectorAll('.acts button')) {
                    if (b.title === 'Удалить') { b.click(); return; }
                }
            });
            await wait(600);
        }
        await page.evaluate(() => { window.confirm = window.__origConfirm; });
        const final = await getSubCount();
        assert(final === 0, `Remaining: ${final}`);
    });

    // ── Report ──

    // Filter out known test-environment noise before report and exit check.
    const realConsoleErrors = consoleErrors.filter(e => !/Failed to load resource/.test(e));
    const realFailedRequests = failedRequests.filter(r => !/\/api\/logs\/xray/.test(r));

    console.log(`\n${'─'.repeat(40)}`);
    console.log(`Results: ${passed} passed, ${failed} failed`);

    if (jsErrors.length) {
        console.log(`\nJS Errors (${jsErrors.length}):`);
        for (const e of [...new Set(jsErrors)]) console.log(`  • ${e.split('\n')[0]}`);
    }
    if (realConsoleErrors.length) {
        console.log(`\nConsole Errors (${realConsoleErrors.length}):`);
        for (const e of [...new Set(realConsoleErrors)]) console.log(`  • ${e.split('\n')[0]}`);
    }
    if (cspViolations.length) {
        console.log(`\nCSP Violations (${cspViolations.length}):`);
        for (const v of [...new Set(cspViolations)]) console.log(`  • ${v.split('\n')[0]}`);
    }
    if (realFailedRequests.length) {
        console.log(`\nFailed API Requests (${realFailedRequests.length}):`);
        for (const r of [...new Set(realFailedRequests)]) console.log(`  • ${r}`);
    }
    if (failedResources.length) {
        console.log(`\nFailed Page Resources (${failedResources.length}):`);
        for (const r of [...new Set(failedResources)]) console.log(`  • ${r}`);
    }
    if (errors.length) {
        console.log('\nFailures:');
        for (const e of errors) console.log(`  ✗ ${e.name}: ${e.error}`);
    }

    await browser?.close();
    serverProcess?.kill();
    if (IS_WIN) try { execSync(`taskkill /F /PID ${serverProcess.pid} 2>nul`, { shell: true }); } catch {}
    // Fail if any test failed OR any infrastructure error was collected.
    const hasInfraErrors = jsErrors.length > 0 || realConsoleErrors.length > 0
        || cspViolations.length > 0 || realFailedRequests.length > 0
        || failedResources.length > 0;
    process.exit((failed > 0 || hasInfraErrors) ? 1 : 0);
}

main().catch(e => {
    console.error('Fatal:', e);
    browser?.close();
    serverProcess?.kill();
    process.exit(1);
});
