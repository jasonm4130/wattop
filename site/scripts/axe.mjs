// Accessibility check: axe-core (WCAG 2.x A/AA rules) against the built site
// served by `astro preview`, at 390 and 1440 px wide, in dark and light.
// Run after `pnpm build`:  pnpm test:a11y
// Exits non-zero on any violation. Set BASE_URL to test an already-running
// server instead of starting a preview.
import { spawn } from 'node:child_process';
import { chromium } from 'playwright';
import { AxeBuilder } from '@axe-core/playwright';

const PAGES = [
  '/',
  '/404',
  ...['', 'install/', 'usage/', 'config/', 'themes/', 'doctor/', 'honest-labelling/', 'pricing/', 'limitations/', 'privacy/', 'troubleshooting/'].map((p) => `/docs/${p}`),
];
const WIDTHS = [390, 1440];
const THEMES = ['dark', 'light'];
const PORT = Number(process.env.PORT || 4329);

let server;
let base = process.env.BASE_URL;
if (!base) {
  base = `http://localhost:${PORT}`;
  // --ignore-lock keeps it in the foreground (Astro otherwise backgrounds
  // the preview server when it detects a coding agent), so killing the
  // process group below always stops it.
  server = spawn('pnpm', ['exec', 'astro', 'preview', '--port', String(PORT), '--ignore-lock'], {
    stdio: 'ignore',
    detached: true,
  });
  const deadline = Date.now() + 30_000;
  for (;;) {
    try {
      if ((await fetch(base + '/')).ok) break;
    } catch {}
    if (Date.now() > deadline) throw new Error('astro preview did not start');
    await new Promise((r) => setTimeout(r, 300));
  }
}

const stop = () => {
  if (server) {
    try {
      process.kill(-server.pid);
    } catch {}
  }
};

let failures = 0;
const browser = await chromium.launch();
try {
  for (const theme of THEMES) {
    for (const width of WIDTHS) {
      const ctx = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce' });
      await ctx.addInitScript((t) => {
        try {
          localStorage.setItem('wattop-theme', t);
        } catch {}
      }, theme);
      const page = await ctx.newPage();
      for (const path of PAGES) {
        await page.goto(base + path, { waitUntil: 'networkidle' });
        const res = await new AxeBuilder({ page })
          .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa', 'best-practice'])
          .analyze();
        const label = `${path} @${width} ${theme}`;
        if (res.violations.length === 0) {
          console.log(`ok   ${label} (${res.passes.length} rules passed)`);
          continue;
        }
        failures += res.violations.length;
        console.log(`FAIL ${label}`);
        for (const v of res.violations) {
          console.log(`  [${v.impact}] ${v.id}: ${v.help}`);
          for (const n of v.nodes.slice(0, 5)) console.log(`      ${n.target.join(' ')}  ${n.failureSummary?.split('\n')[1]?.trim() ?? ''}`);
        }
      }
      await ctx.close();
    }
  }
} finally {
  await browser.close();
  stop();
}
console.log(failures ? `axe: ${failures} violation(s)` : 'axe: 0 violations');
process.exit(failures ? 1 : 0);
