// Post-build: hash every inline <script> body in dist/**/*.html and write the
// hashes into dist/_headers in place of __INLINE_SCRIPT_HASHES__, so the CSP
// can stay script-src 'self' without 'unsafe-inline'. Fails the build if the
// landing page itself has an inline script (it must not).
import { readFileSync, writeFileSync, readdirSync, statSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { join, relative } from 'node:path';

const dist = new URL('../dist/', import.meta.url).pathname;

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) yield* walk(p);
    else if (p.endsWith('.html')) yield p;
  }
}

const hashes = new Set();
const re = /<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/gi;
for (const file of walk(dist)) {
  const html = readFileSync(file, 'utf8');
  for (const m of html.matchAll(re)) {
    const body = m[1];
    if (!body.trim()) continue;
    if (/type="application\/(ld\+)?json"/.test(m[0])) continue;
    const rel = relative(dist, file);
    if (rel === 'index.html' || rel === '404.html') {
      console.error(`csp-hashes: inline script found in ${rel}; the landing page must not have one`);
      process.exit(1);
    }
    hashes.add(`'sha256-${createHash('sha256').update(body).digest('base64')}'`);
  }
}

const headersPath = join(dist, '_headers');
const headers = readFileSync(headersPath, 'utf8');
writeFileSync(headersPath, headers.replace('__INLINE_SCRIPT_HASHES__', [...hashes].join(' ')));
console.log(`csp-hashes: ${hashes.size} inline script hash(es) written to dist/_headers`);
