#!/usr/bin/env node
// Chromium REPL for the factum-dev lab sidecar. One command per stdin line.
//
//   nav <url>                      bare paths resolve against BASE_URL
//   login                          fill+submit the first user/password form
//   click <selector>               chain with " >> " and "nth=N"
//   fill <selector> <text...>
//   press <key>
//   wait-for <selector>
//   screenshot [name]              .grok/skills/factum2-dev/.state/screenshots/
//   viewport <width> <height>
//   eval <js>
//   console-errors
//   quit

import { chromium } from 'playwright-core';
import readline from 'node:readline';
import path from 'node:path';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const SHOT_DIR = process.env.SHOT_DIR || path.join(SCRIPT_DIR, '.state', 'screenshots');
fs.mkdirSync(SHOT_DIR, { recursive: true });

const BASE_URL = process.env.BASE_URL || 'http://factum-web:8091';
const ADMIN_USER = process.env.ADMIN_USER || 'admin';
const ADMIN_PASS = process.env.ADMIN_PASS || 'admin';
const CHROME_PATH = process.env.CHROME_PATH || '/usr/bin/chromium';

const consoleLog = [];

const browser = await chromium.launch({
  executablePath: CHROME_PATH,
  args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-gpu'],
});
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
page.on('pageerror', (e) => consoleLog.push(`[pageerror] ${e}`));
page.on('console', (msg) => {
  if (msg.type() === 'error') consoleLog.push(`[console.error] ${msg.text()}`);
});

function resolveUrl(u) {
  return /^https?:\/\//.test(u) ? u : BASE_URL + (u.startsWith('/') ? u : '/' + u);
}

function resolveLocator(selectorText) {
  const parts = selectorText.split(/\s*>>\s*/);
  let loc = null;
  for (const part of parts) {
    const nthMatch = part.match(/^nth=(\d+)$/);
    if (nthMatch) {
      loc = loc.nth(Number(nthMatch[1]));
    } else {
      loc = loc ? loc.locator(part) : page.locator(part);
    }
  }
  return loc;
}

async function handle(line) {
  const trimmed = line.trim();
  if (!trimmed || trimmed.startsWith('#')) return;
  const [cmd, ...rest] = trimmed.split(/\s+/);
  const arg1 = rest[0];
  const restText = rest.join(' ');

  switch (cmd) {
    case 'nav':
      await page.goto(resolveUrl(arg1), { waitUntil: 'networkidle' });
      console.log('nav ->', page.url());
      break;
    case 'login':
      await page.locator('input[type="text"], input[type="email"]').first().fill(ADMIN_USER);
      await page.locator('input[type="password"]').first().fill(ADMIN_PASS);
      await page
        .locator('button:has-text("Sign In"), button:has-text("Sign in"), button:has-text("Log in"), button[type="submit"]')
        .first()
        .click();
      await page.waitForTimeout(1000);
      console.log('login -> now at', page.url());
      break;
    case 'click':
      await resolveLocator(restText).first().click();
      console.log('clicked', restText);
      break;
    case 'fill': {
      // "fill <selector> -- <text>" so a selector may contain spaces.
      const sep = restText.indexOf(' -- ');
      const selector = sep === -1 ? rest[0] : restText.slice(0, sep);
      const text = sep === -1 ? rest.slice(1).join(' ') : restText.slice(sep + 4);
      await resolveLocator(selector).first().fill(text);
      console.log('filled', selector);
      break;
    }
    case 'force-click':
      await resolveLocator(restText).first().click({ force: true });
      console.log('force-clicked', restText);
      break;
    case 'click-at':
      await page.mouse.click(Number(rest[0]), Number(rest[1]));
      console.log('clicked at', rest[0], rest[1]);
      break;
    case 'press':
      await page.keyboard.press(arg1);
      console.log('pressed', arg1);
      break;
    case 'wait-for':
      await resolveLocator(restText).first().waitFor();
      console.log('found', restText);
      break;
    case 'viewport':
      await page.setViewportSize({ width: Number(rest[0]), height: Number(rest[1] || 900) });
      console.log('viewport', rest[0], rest[1] || 900);
      break;
    case 'screenshot': {
      const name = arg1 || `shot-${Date.now()}`;
      const file = path.join(SHOT_DIR, `${name}.png`);
      await page.screenshot({ path: file });
      console.log('screenshot ->', file);
      break;
    }
    case 'eval': {
      const result = await page.evaluate(restText);
      console.log(JSON.stringify(result));
      break;
    }
    case 'console-errors':
      console.log(consoleLog.length ? consoleLog.join('\n') : '(none)');
      break;
    case 'quit':
      await browser.close();
      process.exit(0);
      break;
    default:
      console.log('unknown command:', cmd);
  }
}

const rl = readline.createInterface({ input: process.stdin });
for await (const line of rl) {
  try {
    await handle(line);
  } catch (err) {
    console.log('ERROR:', err.message);
  }
}
await browser.close();
