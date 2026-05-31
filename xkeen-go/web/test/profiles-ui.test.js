// test/profiles-ui.test.js
// E2E tests for new inline profile UX (TDD Red phase)
// Tests the following behaviors:
//   1. Profile tabs for switching between profiles
//   2. Inline filter editing per active profile (no modal)
//   3. Creating new profiles inline
//   4. Deleting non-default profiles
//   5. Filters update when switching profiles
//
// Usage: cd xkeen-go/web && node test/profiles-ui.test.js

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
const SERVER_BIN = path.join(TMP, 'xkeen-test-profiles', IS_WIN ? 'server.exe' : 'server');
const CONFIG_PATH = path.join(TMP, 'xkeen-test-profiles', 'config', 'config.json');
const PORT = 9878;
const BASE = `http://localhost:${PORT}`;
const PASSWORD = 'password';

let browser, page, serverProcess;
let passed = 0, failed = 0, errors = [];
const jsErrors = [];

function ensureConfig() {
    const configDir = path.join(TMP, 'xkeen-test-profiles', 'config');
    const xrayDir = path.join(TMP, 'xkeen-test-profiles', 'xray');
    fs.mkdirSync(configDir, { recursive: true });
    fs.mkdirSync(xrayDir, { recursive: true });
    // Clean up stale subscription store to avoid leaking state between runs
    try { fs.unlinkSync(path.join(configDir, 'subscriptions.json')); } catch {}
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

async function goToSubscriptions() {
    await page.evaluate(() => {
        const btns = document.querySelectorAll('.nav-btn');
        if (btns[1]) btns[1].click();
    });
    await wait(1000);
}

async function test(name, fn) {
    try { await fn(); passed++; console.log(`  ✓ ${name}`); }
    catch (e) { failed++; errors.push({ name, error: e.message }); console.log(`  ✗ ${name}: ${e.message.split('\n')[0]}`); }
}

// ── Main ──

async function main() {
    console.log('\nE2E: Profile Inline UX Tests\n');
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

    // ── Setup: login and navigate ──

    await test('Login succeeds', async () => {
        await login();
        assert(!page.url().includes('/login'), 'Should redirect from login');
    });

    await test('Navigate to subscriptions tab', async () => {
        await goToSubscriptions();
        assert(await page.$('.sub-wrapper'), 'Subscriptions wrapper exists');
    });

    // ── Profile Tabs ──

    await test('Profile tabs bar renders', async () => {
        const bar = await page.$('.profile-tabs');
        assert(bar, 'Profile tabs bar should exist');
    });

    await test('Default profile tab is active', async () => {
        const activeTab = await page.evaluate(() => {
            const active = document.querySelector('.profile-tabs .ptab.active');
            return active ? active.textContent.trim() : null;
        });
        assert(activeTab, 'Should have an active profile tab');
        assert(activeTab.includes('По умолчанию') || activeTab.includes('default'),
            `Active tab should be default profile, got: "${activeTab}"`);
    });

    await test('Add-profile button exists in tabs bar', async () => {
        const btn = await page.$('.profile-tabs .ptab-add');
        assert(btn, 'Add profile button (+) should exist');
    });

    // ── Inline Filters for Active Profile ──

    await test('Filters section shows for active profile', async () => {
        // The .sub-filters section should always be visible (even without proxies)
        // showing the active profile's filter controls
        const filters = await page.$('.sub-filters');
        assert(filters, 'Filters section should be visible');
    });

    await test('Strategy pills visible in filters section', async () => {
        const count = await page.evaluate(() =>
            document.querySelectorAll('.sub-filters .strat-pills .spill').length
        );
        assert(count >= 4, `Should have strategy pills, got ${count}`);
    });

    await test('Strategy can be changed inline', async () => {
        const result = await page.evaluate(() => {
            const pills = document.querySelectorAll('.sub-filters .strat-pills .spill');
            // Click "Случайный" (random)
            for (const p of pills) {
                if (p.textContent.includes('Случайный') && !p.classList.contains('active')) {
                    p.click();
                    return { clicked: true, text: p.textContent.trim() };
                }
            }
            return { clicked: false };
        });
        assert(result.clicked, 'Should click random strategy pill');
        await wait(500);
        const isActive = await page.evaluate(() => {
            const pills = document.querySelectorAll('.sub-filters .strat-pills .spill');
            for (const p of pills) {
                if (p.textContent.includes('Случайный')) return p.classList.contains('active');
            }
            return false;
        });
        assert(isActive, 'Random strategy should be active after click');
    });

    await test('Regex sections visible in filters section', async () => {
        const result = await page.evaluate(() => {
            const incLabel = [...document.querySelectorAll('.sub-filters .sub-row-label')]
                .some(el => el.textContent.includes('Regex включения'));
            const excLabel = [...document.querySelectorAll('.sub-filters .sub-row-label')]
                .some(el => el.textContent.includes('Regex исключения'));
            const addBtns = document.querySelectorAll('.sub-filters .rpill-add').length;
            const maxInput = document.querySelector('.sub-filters input[placeholder*="Лимит"]') ||
                             document.querySelector('.sub-filters input[type="number"]');
            return { incLabel, excLabel, addBtns, hasMax: !!maxInput };
        });
        assert(result.incLabel, 'Should have include regex label');
        assert(result.excLabel, 'Should have exclude regex label');
        assert(result.addBtns >= 2, `Should have 2+ regex add buttons, got ${result.addBtns}`);
        assert(result.hasMax, 'Should have max proxies input');
    });

    await test('Can add regex rule via pill button', async () => {
        // Click the first rpill-add button (include regex)
        await page.evaluate(() => {
            const btn = document.querySelector('.sub-filters .rpill-add');
            if (btn) btn.click();
        });
        await wait(200);
        // Type a regex pattern and press Enter
        const typed = await page.evaluate(() => {
            const input = document.querySelector('.sub-filters .rpill-input');
            if (!input) return false;
            const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
            nativeInputValueSetter.call(input, 'Fast');
            input.dispatchEvent(new Event('input', { bubbles: true }));
            input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
            return true;
        });
        assert(typed, 'Should type regex and press Enter');
        await wait(500);
        // Check that a pill appeared
        const pillCount = await page.evaluate(() =>
            document.querySelectorAll('.sub-filters .rpill:not(.rpill-excl)').length
        );
        assert(pillCount >= 1, `Should have 1+ include regex pill, got ${pillCount}`);
    });

    // ── Create Profile Inline ──

    await test('Click add profile shows inline name input', async () => {
        await page.evaluate(() => {
            const btn = document.querySelector('.profile-tabs .ptab-add');
            if (btn) btn.click();
        });
        await wait(300);
        const hasInput = await page.evaluate(() => {
            const input = document.querySelector('.profile-tabs .ptab-new-input') ||
                          document.querySelector('.profile-tabs input.new-profile-name');
            return !!input;
        });
        assert(hasInput, 'Should show inline name input for new profile');
    });

    await test('Type name and confirm creates new profile tab', async () => {
        const created = await page.evaluate(() => {
            const input = document.querySelector('.profile-tabs .ptab-new-input') ||
                          document.querySelector('.profile-tabs input.new-profile-name');
            if (!input) return false;
            const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
            nativeInputValueSetter.call(input, 'Test Profile');
            input.dispatchEvent(new Event('input', { bubbles: true }));
            input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
            return true;
        });
        assert(created, 'Should type name and press Enter');
        await wait(800);

        const tabCount = await page.evaluate(() =>
            document.querySelectorAll('.profile-tabs .ptab:not(.ptab-add)').length
        );
        assert(tabCount >= 2, `Should have 2 profile tabs (default + new), got ${tabCount}`);
    });

    await test('New profile tab becomes active after creation', async () => {
        const activeText = await page.evaluate(() => {
            const active = document.querySelector('.profile-tabs .ptab.active');
            return active ? active.textContent.trim() : null;
        });
        assert(activeText && activeText.includes('Test'),
            `New profile tab should be active, got: "${activeText}"`);
    });

    await test('Filters section shows new profile name', async () => {
        const name = await page.evaluate(() => {
            const label = document.querySelector('.sub-filters .profile-filter-name') ||
                          document.querySelector('.sub-filters .sub-row-label');
            return label ? label.textContent.trim() : null;
        });
        // The filters section should reflect the active profile
        assert(name, 'Filters section should show profile identifier');
    });

    // ── Switch Profile Tabs ──

    await test('Switch to default profile tab', async () => {
        await page.evaluate(() => {
            const tabs = document.querySelectorAll('.profile-tabs .ptab:not(.ptab-add)');
            for (const t of tabs) {
                if (t.textContent.includes('По умолчанию') || t.textContent.includes('default')) {
                    t.click();
                    return;
                }
            }
            // fallback: click first tab
            if (tabs[0]) tabs[0].click();
        });
        await wait(500);
        const isActive = await page.evaluate(() => {
            const active = document.querySelector('.profile-tabs .ptab.active');
            return active ? (active.textContent.includes('По умолчанию') || active.textContent.includes('default')) : false;
        });
        assert(isActive, 'Default profile tab should be active');
    });

    // ── Delete Profile Inline ──

    await test('Delete button exists on non-default profile', async () => {
        // Switch to Test Profile first
        await page.evaluate(() => {
            const tabs = document.querySelectorAll('.profile-tabs .ptab:not(.ptab-add)');
            for (const t of tabs) {
                if (t.textContent.includes('Test')) { t.click(); return; }
            }
        });
        await wait(300);

        const hasDelete = await page.evaluate(() => {
            const btn = document.querySelector('.sub-filters .profile-delete-btn') ||
                        document.querySelector('.sub-filters button[title*="Удалить"]') ||
                        document.querySelector('.profile-tabs .ptab.active .ptab-delete');
            return !!btn;
        });
        assert(hasDelete, 'Non-default profile should have delete button');
    });

    await test('Delete non-default profile removes tab', async () => {
        await page.evaluate(() => { window.__origConfirm = window.confirm; window.confirm = () => true; });

        await page.evaluate(() => {
            const btn = document.querySelector('.sub-filters .profile-delete-btn') ||
                        document.querySelector('.sub-filters button[title*="Удалить"]') ||
                        document.querySelector('.profile-tabs .ptab.active .ptab-delete');
            if (btn) btn.click();
        });
        await wait(800);

        await page.evaluate(() => { window.confirm = window.__origConfirm; });

        const tabCount = await page.evaluate(() =>
            document.querySelectorAll('.profile-tabs .ptab:not(.ptab-add)').length
        );
        assert(tabCount === 1, `Should have 1 tab after delete, got ${tabCount}`);
    });

    // ── No Profile Modal ──

    await test('Profile editor modal does not exist in DOM', async () => {
        const hasModal = await page.evaluate(() =>
            !!document.querySelector('.modal-overlay .modal-box h3') &&
            document.querySelector('.modal-overlay .modal-box') !== null
        );
        // The profile modal markup should be removed entirely
        // We check that there's no profile-specific modal
        const profileModalText = await page.evaluate(() => {
            const modals = document.querySelectorAll('.modal-overlay .modal-box h3');
            for (const m of modals) {
                const text = m.textContent || '';
                if (text.includes('Новый профиль') || text.includes('Редактировать профиль') || text.includes('Профиль по умолчанию')) {
                    return text;
                }
            }
            return null;
        });
        assert(!profileModalText, `Profile modal should not exist, found: "${profileModalText}"`);
    });

    // ── No JS Errors ──

    await test('No JS errors during profile operations', async () => {
        const bad = jsErrors.filter(e => /vue|profile|subscription|component/i.test(e));
        assert(bad.length === 0, `Vue/component errors: ${bad.join('; ')}`);
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
