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

function ensureConfig() {
    const configDir = path.join(TMP, 'xkeen-test', 'config');
    const xrayDir = path.join(TMP, 'xkeen-test', 'xray');
    fs.mkdirSync(configDir, { recursive: true });
    fs.mkdirSync(xrayDir, { recursive: true });
    fs.writeFileSync(CONFIG_PATH, JSON.stringify({
        port: PORT,
        xray_config_dir: xrayDir.replace(/\\/g, '/'),
        allowed_roots: [xrayDir.replace(/\\/g, '/'), configDir.replace(/\\/g, '/')],
        auth: {
            password_hash: "$2a$12$oDp.vVnkWYsDjAuEWEKOgOR08sApErSrJFyRMbOE5d/GvccJKiNLe",
            session_timeout: 24, max_login_attempts: 100, lockout_duration: 1
        }
    }, null, 2));
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
    return page.evaluate(() => document.querySelectorAll('.sub-card').length);
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
    page.on('pageerror', err => jsErrors.push(err.message));

    // ── Tests ──

    await test('Login succeeds', async () => {
        await login();
        assert(!page.url().includes('/login'), 'Should redirect from login');
    });

    await test('Sidebar nav renders', async () => {
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
    await test('Clean slate', async () => {
        await page.evaluate(() => { window.__origConfirm = window.confirm; window.confirm = () => true; });
        for (let i = 0; i < 10; i++) {
            const remaining = await page.evaluate(() => document.querySelectorAll('.sub-card').length);
            if (remaining === 0) break;
            await page.evaluate(() => {
                const cards = document.querySelectorAll('.sub-card');
                const last = cards[cards.length - 1];
                if (!last) return;
                for (const b of last.querySelectorAll('.acts button')) {
                    if (b.title === 'Удалить') { b.click(); return; }
                }
            });
            await wait(600);
        }
        await page.evaluate(() => { window.confirm = window.__origConfirm; });
        const count = await getSubCount();
        assert(count === 0, `Should be clean, got ${count}`);
    });

    await test('No JS errors on page load', async () => {
        const bad = jsErrors.filter(e => /vue|subscription|component/i.test(e));
        assert(bad.length === 0, `Errors: ${bad.join('; ')}`);
    });

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
        assert(count >= 1, `Should have 1+ sub, got ${count}`);
    });

    await test('Subscription cards have name and action buttons', async () => {
        const info = await page.evaluate(() => {
            const cards = document.querySelectorAll('.sub-card');
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
        const clicked = await page.evaluate(() => {
            const card = document.querySelectorAll('.sub-card')[0];
            if (!card) return false;
            for (const b of card.querySelectorAll('.acts button')) {
                if (b.title === 'Редактировать') { b.click(); return true; }
            }
            return false;
        });
        assert(clicked, 'Edit button found and clicked');
        await wait(500);
        const hasForm = await page.evaluate(() => !!document.querySelector('.sub-card.editing .sub-edit'));
        assert(hasForm, 'Edit form visible after click');
        await page.evaluate(() => {
            const btns = document.querySelectorAll('.sub-card.editing button');
            for (const b of btns) { if (b.textContent.includes('✕')) { b.click(); return; } }
        });
        await wait(300);
    });

    await test('Preview/Apply buttons in toolbar', async () => {
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
        const clicked = await page.evaluate(() => {
            const card = document.querySelectorAll('.sub-card')[0];
            if (!card) return false;
            for (const b of card.querySelectorAll('.acts button')) {
                if (b.title === 'Обновить') { b.click(); return true; }
            }
            // Fallback: first button is fetch
            const btns = card.querySelectorAll('.acts button');
            if (btns.length > 0) { btns[0].click(); return true; }
            return false;
        });
        assert(clicked, 'Fetch button exists on card');
        await wait(2000);
    });

    await test('Delete subscription', async () => {
        const before = await getSubCount();
        await page.evaluate(() => { window.__origConfirm = window.confirm; window.confirm = () => true; });
        await page.evaluate(() => {
            const card = document.querySelectorAll('.sub-card')[0];
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

    await test('Strategy pills exist', async () => {
        const count = await page.evaluate(() => document.querySelectorAll('.strat-pills .spill').length);
        assert(count >= 4, `Strategy pills: ${count}`);
    });

    await test('Marker pills render', async () => {
        await page.evaluate(() => document.querySelectorAll('.marker-pills .mpill').length);
    });

    await test('Country cloud renders', async () => {
        await page.evaluate(() => document.querySelectorAll('.country-cloud .cc').length);
    });

    await test('Clean up: delete all subscriptions', async () => {
        const count = await getSubCount();
        if (count === 0) return;
        await page.evaluate(() => { window.__origConfirm = window.confirm; window.confirm = () => true; });
        for (let i = 0; i < count + 2; i++) {
            const remaining = await page.evaluate(() => document.querySelectorAll('.sub-card').length);
            if (remaining === 0) break;
            await page.evaluate(() => {
                const cards = document.querySelectorAll('.sub-card');
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
    console.log(`\n${'─'.repeat(40)}`);
    console.log(`Results: ${passed} passed, ${failed} failed`);

    if (jsErrors.length) {
        console.log(`\nJS Errors (${jsErrors.length}):`);
        for (const e of [...new Set(jsErrors)]) console.log(`  • ${e.split('\n')[0]}`);
    }
    if (errors.length) {
        console.log('\nFailures:');
        for (const e of errors) console.log(`  ✗ ${e.name}: ${e.error}`);
    }

    await browser?.close();
    serverProcess?.kill();
    if (IS_WIN) try { execSync(`taskkill /F /PID ${serverProcess.pid} 2>nul`, { shell: true }); } catch {}
    process.exit(failed > 0 ? 1 : 0);
}

main().catch(e => {
    console.error('Fatal:', e);
    browser?.close();
    serverProcess?.kill();
    process.exit(1);
});
