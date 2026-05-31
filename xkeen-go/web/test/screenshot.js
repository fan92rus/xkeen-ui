import puppeteer from 'puppeteer';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const shotsDir = path.join(__dirname, 'screenshots');
if (!fs.existsSync(shotsDir)) fs.mkdirSync(shotsDir, { recursive: true });

const BASE = 'http://localhost:9877';

async function login(page) {
    await page.goto(`${BASE}/login`, { waitUntil: 'networkidle0' });
    await page.type('input[type="password"]', 'password');
    await page.click('button[type="submit"]');
    await page.waitForNavigation({ waitUntil: 'networkidle0' });
}

async function screenshot(page, name) {
    const file = path.join(shotsDir, `${name}.png`);
    await page.screenshot({ path: file, fullPage: false });
    console.log(`Saved: ${name}.png`);
}

const browser = await puppeteer.launch({ headless: 'new', args: ['--no-sandbox'] });
const page = await browser.newPage();
await page.setViewport({ width: 1280, height: 800 });

await login(page);

// Wait for Vue to mount
await page.waitForSelector('.nav-btn', { timeout: 5000 });

const tabs = ['editor', 'settings', 'subscriptions', 'logs', 'commands'];
const tabNames = { editor: 0, settings: 3, subscriptions: 1, logs: 2, commands: 4 };

for (const [name, idx] of Object.entries(tabNames)) {
    const btns = await page.$$('.nav-btn');
    if (btns[idx]) {
        await btns[idx].click();
        await new Promise(r => setTimeout(r, 500));
    }
    await screenshot(page, `${name}-tab`);
}

// Login page
await page.goto(`${BASE}/login`, { waitUntil: 'networkidle0' });
await screenshot(page, 'login-page');

await browser.close();
console.log('Done');
