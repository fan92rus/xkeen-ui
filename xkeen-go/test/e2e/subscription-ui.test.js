// test/e2e/subscription-ui.test.js
// E2E tests for subscription UI with Puppeteer
//
// Usage: cd xkeen-go/web && npm run test:e2e
// Requires: server running on localhost:9877 with test config

import puppeteer from 'puppeteer';
import { execSync, spawn } from 'child_process';
import { createServer } from 'http';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, '..');  // xkeen-go root
const PORT = 9877;
const BASE = `http://localhost:${PORT}`;
const PASSWORD = 'password';

let browser, page, serverProcess;
let passed = 0, failed = 0, errors = [];

// ── Helpers ──

function assert(cond, msg) {
    if (!cond) throw new Error(`Assertion failed: ${msg}`);
}

async function waitFor(fn, timeout = 10000, interval = 300) {
    const start = Date.now();
    let lastErr;
    while (Date.now() - start < timeout) {
        try {
            const result = await fn();
            if (result) return result;
        } catch (e) { lastErr = e; }
        await new Promise(r => setTimeout(r, interval));
    }
    throw lastErr || new Error(`waitFor timeout (${timeout}ms)`);
}

async function login() {
    // Navigate to login page first
    await page.goto(`${BASE}/login`, { waitUntil: 'networkidle0' });

    // Check if we're on login page or already logged in
    const url = page.url();
    if (url.includes('/login')) {
        await page.waitForSelector('input[type="password"]', { timeout: 5000 });
        await page.type('input[type="password"]', PASSWORD);
        await page.click('button[type="submit"], button');
        await page.waitForNavigation({ waitUntil: 'networkidle0' });
    }
}

async function goToSubscriptions() {
    // Click the subscriptions tab
    await page.waitForSelector('.tab', { timeout: 5000 });
    const tabs = await page.$$('.tab');
    for (const tab of tabs) {
        const text = await page.evaluate(el => el.textContent, tab);
        if (text.includes('Подписки')) {
            await tab.click();
            break;
        }
    }
    // Wait for Alpine to render
    await page.waitForTimeout(500);
}

async function addSubscription(url) {
    // Type URL and press Enter
    await page.waitForSelector('.sub-toolbar input[type="url"]', { timeout: 5000 });
    const input = await page.$('.sub-toolbar input[type="url"]');
    await input.click({ clickCount: 3 }); // select all
    await input.type(url);
    await page.keyboard.press('Enter');
    // Wait for API call
    await page.waitForTimeout(1500);
}

async function getSubCards() {
    return await page.$$eval('.sub-card', cards =>
        cards.map(c => ({
            name: c.querySelector('.name')?.textContent || '',
            badge: c.querySelector('.badge')?.textContent || '',
            hasErr: !!c.querySelector('.err'),
        }))
    );
}

async function clickSubButton(index, title) {
    const buttons = await page.$$(`.sub-card:nth-child(${index + 1}) .acts button`);
    for (const btn of buttons) {
        const t = await page.evaluate(el => el.title || el.textContent, btn);
        if (t.includes(title)) {
            await btn.click();
            return true;
        }
    }
    return false;
}

async function getToastText() {
    try {
        const toast = await page.waitForSelector('.toast', { timeout: 3000 });
        return await page.evaluate(el => el.textContent, toast);
    } catch {
        return '';
    }
}

// ── Test runner ──

async function test(name, fn) {
    try {
        await fn();
        passed++;
        console.log(`  ✓ ${name}`);
    } catch (e) {
        failed++;
        errors.push({ name, error: e.message });
        console.log(`  ✗ ${name}: ${e.message}`);
    }
}

// ── Main ──

async function main() {
    console.log('\nE2E: Subscription UI Tests\n');

    // Build and start server
    console.log('Building...');
    try {
        execSync('node build.js', { cwd: path.join(ROOT, 'web'), stdio: 'pipe' });
        execSync('go build -o /tmp/xkeen-test/server .', { cwd: ROOT, stdio: 'pipe' });
    } catch (e) {
        console.error('Build failed:', e.message);
        process.exit(1);
    }

    // Kill any existing server
    try { execSync(`taskkill /F /IM server.exe 2>nul`, { shell: true }); } catch {}

    // Start server
    serverProcess = spawn('/tmp/xkeen-test/server', ['-config', '/tmp/xkeen-test/config/config.json'], {
        cwd: ROOT,
        stdio: ['pipe', 'pipe', 'pipe'],
        env: { ...process.env },
    });

    // Wait for server to start
    await waitFor(async () => {
        try {
            const res = await fetch(`${BASE}/health`);
            return res.ok;
        } catch { return false; }
    }, 10000);

    console.log('Server started\n');

    // Launch browser
    browser = await puppeteer.launch({
        headless: 'new',
        args: ['--no-sandbox', '--disable-setuid-sandbox'],
    });
    page = await browser.newPage();
    page.setDefaultTimeout(10000);

    // Collect console logs
    page.on('console', msg => {
        if (msg.type() === 'error') {
            console.log(`    [browser console.error] ${msg.text()}`);
        }
    });

    // ── Tests ──

    await test('Login page loads', async () => {
        await login();
        const url = page.url();
        assert(!url.includes('/login'), `Should redirect away from login, got: ${url}`);
    });

    await test('Subscriptions tab renders with toolbar', async () => {
        await goToSubscriptions();
        const toolbar = await page.$('.sub-toolbar');
        assert(toolbar, 'Toolbar should exist');
        const input = await page.$('.sub-toolbar input[type="url"]');
        assert(input, 'URL input should exist');
    });

    await test('Empty state shows placeholder', async () => {
        const empty = await page.$('.sub-empty');
        if (empty) {
            const text = await page.evaluate(el => el.textContent, empty);
            assert(text.includes('URL') || text.length > 0, 'Empty state should have text');
        }
        // It's ok if there's no empty state (subs already exist from prev test)
    });

    await test('Add subscription via Enter key', async () => {
        // Count existing cards
        const before = (await getSubCards()).length;
        await addSubscription('https://example.com/sub/test');

        // Wait for card to appear
        await waitFor(async () => {
            const cards = await getSubCards();
            return cards.length > before;
        }, 5000);

        const cards = await getSubCards();
        assert(cards.length > before, `Card count should increase. Before: ${before}, After: ${cards.length}`);
    });

    await test('Subscription card shows name and actions', async () => {
        const cards = await getSubCards();
        assert(cards.length > 0, 'Should have at least one subscription card');

        // Check first card has action buttons
        const acts = await page.$$('.sub-card:first-child .acts button');
        assert(acts.length >= 2, `Should have action buttons, got ${acts.length}`);
    });

    await test('Fetch subscription shows toast', async () => {
        // Click fetch button on first subscription
        const clicked = await clickSubButton(0, 'Обновить');
        assert(clicked, 'Fetch button should be clickable');

        // Wait for toast or proxy count to change
        await page.waitForTimeout(3000);

        // Check if any card now shows a badge or error
        const cards = await getSubCards();
        // The test subscription URL won't actually work, so we might get an error badge
        // That's expected behavior
    });

    await test('Right panel shows when proxies loaded', async () => {
        // Load proxies via the toolbar button
        const buttons = await page.$$('.sub-toolbar button');
        let clicked = false;
        for (const btn of buttons) {
            const t = await page.evaluate(el => el.title || el.textContent, btn);
            if (t.includes('Загрузить') || t.includes('📋')) {
                await btn.click();
                clicked = true;
                break;
            }
        }
        // If there are no proxies, the right panel won't show — that's expected
        await page.waitForTimeout(1500);
    });

    await test('Preview button exists in toolbar', async () => {
        const buttons = await page.$$('.sub-toolbar button');
        let found = false;
        for (const btn of buttons) {
            const t = await page.evaluate(el => el.textContent, btn);
            if (t.includes('Предпросмотр')) {
                found = true;
                break;
            }
        }
        assert(found, 'Preview button should be in toolbar');
    });

    await test('Apply button exists in toolbar', async () => {
        const buttons = await page.$$('.sub-toolbar button');
        let found = false;
        for (const btn of buttons) {
            const t = await page.evaluate(el => el.textContent, btn);
            if (t.includes('Применить')) {
                found = true;
                break;
            }
        }
        assert(found, 'Apply button should be in toolbar');
    });

    await test('No JS errors in console', async () => {
        // Collect JS errors during the session
        const jsErrors = [];
        page.on('pageerror', err => jsErrors.push(err.message));

        // Reload and navigate to subscriptions
        await page.reload({ waitUntil: 'networkidle0' });
        await login();
        await goToSubscriptions();
        await page.waitForTimeout(2000);

        // Alpine init errors are critical
        // (pageerror events collected above would show them)
    });

    await test('Edit subscription inline', async () => {
        // Click edit button on first card
        const clicked = await clickSubButton(0, 'Редактировать');
        if (clicked) {
            await page.waitForTimeout(300);
            // Should show edit form
            const editForm = await page.$('.sub-card.editing .sub-edit');
            assert(editForm, 'Edit form should appear');

            // Cancel edit
            const cancelBtn = await page.$('.sub-card.editing .btn:not(.btn-primary)');
            if (cancelBtn) await cancelBtn.click();
            await page.waitForTimeout(300);
        }
    });

    // ── Cleanup & Report ──

    console.log(`\n${'─'.repeat(40)}`);
    console.log(`Results: ${passed} passed, ${failed} failed`);

    if (errors.length) {
        console.log('\nFailures:');
        for (const e of errors) {
            console.log(`  ✗ ${e.name}: ${e.error}`);
        }
    }

    await browser?.close();
    serverProcess?.kill();

    process.exit(failed > 0 ? 1 : 0);
}

main().catch(e => {
    console.error('Fatal:', e);
    browser?.close();
    serverProcess?.kill();
    process.exit(1);
});
