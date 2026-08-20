#!/usr/bin/env node
// Minimal chromium-cli-alike REPL, for machines (like this one) that don't
// have chromium-cli installed. Reads commands from stdin, one per line:
//
//   nav <url>                      navigate (bare paths are relative to BASE_URL)
//   login                          fill+submit the login form with admin creds and wait for redirect
//   click <selector>                first match; chain with " >> " and
//                                    "nth=N" segments to drill into the
//                                    Nth match at any point, e.g.:
//                                    table >> nth=1 >> tbody tr >> nth=0 >> button
//   fill <selector> <text...>
//   press <key>                    e.g. Enter
//   wait-for <selector>
//   screenshot [name]               saved to .grok/skills/run-factum2-web/.state/screenshots/
//   eval <js>                       runs in page context, result printed as JSON
//   console-errors                  print collected console.error/pageerror text so far
//   quit
//
// Requires setup.sh to have been run first (backend up on BASE_URL).
// Usage:
//   node .grok/skills/run-factum2-web/browser.mjs <<'EOF'
//   nav /login
//   login
//   nav /sync/status
//   screenshot job-status
//   EOF

import { chromium } from 'playwright-core';
import readline from 'node:readline';
import path from 'node:path';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const SHOT_DIR = path.join(SCRIPT_DIR, '.state', 'screenshots');
fs.mkdirSync(SHOT_DIR, { recursive: true });

const BASE_URL = process.env.BASE_URL || 'http://127.0.0.1:18090';
const ADMIN_USER = process.env.ADMIN_USER || 'admin@local';
const ADMIN_PASS = process.env.ADMIN_PASS || 'skilltest123';
const CHROME_PATH = process.env.CHROME_PATH || '/usr/bin/google-chrome-stable';

const consoleLog = [];

const browser = await chromium.launch({
  executablePath: CHROME_PATH,
  args: ['--no-sandbox'],
});
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
page.on('pageerror', (e) => consoleLog.push(`[pageerror] ${e}`));
page.on('console', (msg) => {
  if (msg.type() === 'error') consoleLog.push(`[console.error] ${msg.text()}`);
});

function resolveUrl(u) {
  return /^https?:\/\//.test(u) ? u : BASE_URL + (u.startsWith('/') ? u : '/' + u);
}

// Splits a selector on ' >> ' and reduces it into chained .locator()/.nth()
// calls ourselves, rather than relying on Playwright's own engine-prefixed
// ">> nth=" string syntax (which needs a "css=" prefix on every segment to
// work reliably and still misbehaves when chained more than once - use
// this instead, e.g. "table >> nth=1 >> tbody tr >> nth=0 >> button").
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
        .locator('button:has-text("Sign In"), button:has-text("Sign in"), button[type="submit"]')
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
      const [selector, ...text] = rest;
      await resolveLocator(selector).first().fill(text.join(' '));
      console.log('filled', selector);
      break;
    }
    case 'press':
      await page.keyboard.press(arg1);
      console.log('pressed', arg1);
      break;
    case 'wait-for':
      await resolveLocator(restText).first().waitFor();
      console.log('found', restText);
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
