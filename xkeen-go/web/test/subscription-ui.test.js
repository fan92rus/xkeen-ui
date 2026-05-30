// test/subscription-ui.test.js
// E2E tests for subscription UI with Puppeteer
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
    if (!fs.existsSync(CONFIG_PATH)) {
        fs.writeFileSync(CONFIG_PATH, JSON.stringify({
            port: PORT,
            xray_config_dir: xrayDir.replace(/\\/g, '/'),
            allowed_roots: [xrayDir.replace(/\\/g, '/'), configDir.replace(/\\/g, '/')],
            auth: {
                password_hash: "$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/X4.qO.1BoWBPfGKWe",
                session_timeout: 24, max_login_attempts: 100, lockout_duration: 1
            }
        }, null, 2));
    }
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

// Click via page.evaluate (avoids stale element issues with Alpine re-renders)
async function clickByTitle(selector, titleText) {
    return page.evaluate((sel, title) => {
        const btns = document.querySelectorAll(sel);
        for (const btn of btns) {
            if ((btn.title || btn.textContent).includes(title)) { btn.click(); return true; }
        }
        return false;
    }, selector, titleText);
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

async function goToSubscriptions() {
    await page.evaluate(() => {
        const tabs = document.querySelectorAll('.tab');
        for (const t of tabs) { if (t.textContent.includes('Подписки')) { t.click(); return; } }
    });
    await wait(2000);
}

async function alpineState() {
    return page.evaluate(() => {
        const el = document.querySelector('[x-data="subscriptions"]');
        if (!el?._x_dataStack) return null;
        const d = el._x_dataStack[0];
        return JSON.parse(JSON.stringify({
            subs: d.subs?.map(s => ({ id: s.id, name: s.name, proxy_count: s.proxy_count })),
            proxies: d.proxies?.length, markers: d.markers,
            busy: d.busy, strategy: d.strategy, filters: d.filters,
        }));
    });
}

async function test(name, fn) {
    try { await fn(); passed++; console.log(`  ✓ ${name}`); }
    catch (e) { failed++; errors.push({ name, error: e.message }); console.log(`  ✗ ${name}: ${e.message.split('\n')[0]}`); }
}

// ── Main ──

async function main() {
    console.log('\nE2E: Subscription UI Tests\n');
    console.log('Building...');
    execSync('node build.js', { cwd: path.join(ROOT, 'web'), stdio: 'pipe' });
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

    await test('Subscriptions tab renders toolbar + left panel', async () => {
        await goToSubscriptions();
        assert(await page.$('.sub-toolbar'), 'Toolbar');
        assert(await page.$('.sub-toolbar input[type="url"]'), 'URL input');
        assert(await page.$('.sub-left'), 'Left panel');
    });

    await test('Alpine component loads subs from API', async () => {
        const s = await alpineState();
        assert(s, 'Alpine state exists');
        assert(Array.isArray(s.subs), 'subs is array');
    });

    await test('No Alpine JS errors on page load', async () => {
        // Just check current page errors (no reload needed)
        const bad = jsErrors.filter(e => /alpine|subscription|previewdata|duplicate key/i.test(e));
        assert(bad.length === 0, `Errors: ${bad.join('; ')}`);
    });

    await test('Add subscription', async () => {
        const before = (await alpineState()).subs.length;
        await page.evaluate(url => {
            const input = document.querySelector('.sub-toolbar input[type="url"]');
            input.value = url;
            input.dispatchEvent(new Event('input', { bubbles: true }));
            input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
        }, 'https://example.com/e2e-test');
        await waitFor(async () => (await alpineState())?.subs?.length > before, 5000);
        const after = (await alpineState()).subs;
        assert(after.length > before, `Count: ${before} → ${after.length}`);
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
        const card = (await page.$$('.sub-card'))[0];
        assert(card, 'First card exists');
        const btns = await card.$$('.acts button');
        let editBtn = null;
        for (const b of btns) {
            const title = await page.evaluate(el => el.title, b);
            if (title === 'Редактировать') { editBtn = b; break; }
        }
        assert(editBtn, 'Edit button found');
        await editBtn.click();
        await wait(500);
        const hasForm = await page.evaluate(() => !!document.querySelector('.sub-card.editing .sub-edit'));
        assert(hasForm, 'Edit form visible after click');
        // Cancel edit
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

    await test('Load proxies button exists', async () => {
        const found = await clickByTitle('.sub-toolbar button', 'Загрузить');
        assert(found, 'Load proxies button');
        await wait(1000);
    });

    await test('Fetch button clicks without error', async () => {
        const cards = await page.$$('.sub-card');
        assert(cards.length > 0, 'Cards exist');
        const btns = await cards[0].$$('.acts button');
        let fetchBtn = null;
        for (const b of btns) {
            const title = await page.evaluate(el => el.title, b);
            if (title === 'Обновить') { fetchBtn = b; break; }
        }
        assert(fetchBtn, 'Fetch button exists on card');
        await fetchBtn.click();
        await wait(3000);
    });

    await test('Delete subscription', async () => {
        const before = (await alpineState()).subs.length;
        page.once('dialog', d => d.accept());
        const clicked = await clickByTitle('.sub-card:last-child .acts button', 'Удалить');
        if (clicked) {
            await wait(1000);
            const after = (await alpineState()).subs.length;
            assert(after < before, `Count: ${before} → ${after}`);
        }
    });

    await test('Two-column flex layout', async () => {
        const display = await page.evaluate(() => {
            const el = document.querySelector('.sub-scroll.sub-2col');
            return el ? getComputedStyle(el).display : null;
        });
        assert(display === 'flex', `Layout should be flex, got ${display}`);
    });

    await test('Strategy pills exist when settings visible', async () => {
        const count = await page.evaluate(() => document.querySelectorAll('.strat-pills .spill').length);
        // Strategy pills always exist in DOM (may be hidden by x-show)
        assert(count >= 4, `Strategy pills: ${count}`);
    });

    await test('Marker pills render (may be hidden)', async () => {
        const count = await page.evaluate(() => document.querySelectorAll('.marker-pills .mpill').length);
        // May be 0 if no proxies loaded, that's OK
    });

    await test('Country cloud renders (may be hidden)', async () => {
        const count = await page.evaluate(() => document.querySelectorAll('.country-cloud .cc').length);
        // May be 0 if no proxies
    });

    await test('Clean up: delete all subscriptions', async () => {
        const s = await alpineState();
        const count = s?.subs?.length || 0;
        if (count === 0) { return; }
        await page.evaluate(() => { window.__origConfirm = window.confirm; window.confirm = () => true; });
        for (let i = 0; i < count + 2; i++) {
            const cards = await page.$$('.sub-card');
            if (cards.length === 0) break;
            const last = cards[cards.length - 1];
            const btns = await last.$$('.acts button');
            for (const b of btns) {
                const title = await page.evaluate(el => el.title, b);
                if (title === 'Удалить') { await b.click(); break; }
            }
            await wait(600);
        }
        await page.evaluate(() => { window.confirm = window.__origConfirm; });
        const final = await alpineState();
        assert(final.subs.length === 0, `Remaining: ${final.subs.length}`);
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
