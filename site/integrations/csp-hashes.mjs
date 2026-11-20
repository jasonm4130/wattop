import { readFileSync, writeFileSync, readdirSync, statSync, existsSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const PLACEHOLDER = '__INLINE_SCRIPT_HASHES__';

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) yield* walk(p);
    else if (p.endsWith('.html')) yield p;
  }
}

export function writeCspHashes(dist) {
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
        throw new Error(`csp-hashes: inline script found in ${rel}; the landing and 404 pages must not have one`);
      }
      hashes.add(`'sha256-${createHash('sha256').update(body).digest('base64')}'`);
    }
  }
  const headersPath = join(dist, '_headers');
  if (!existsSync(headersPath)) throw new Error('csp-hashes: dist/_headers is missing');
  const headers = readFileSync(headersPath, 'utf8');
  if (!headers.includes(PLACEHOLDER)) throw new Error(`csp-hashes: dist/_headers has no ${PLACEHOLDER}`);
  if (hashes.size === 0) throw new Error('csp-hashes: found 0 inline script hashes; expected Starlight\'s');
  writeFileSync(headersPath, headers.replace(PLACEHOLDER, [...hashes].join(' ')));
  return hashes;
}

export default function cspHashes() {
  return {
    name: 'wattop:csp-hashes',
    hooks: {
      'astro:build:done': ({ dir, logger }) => {
        const hashes = writeCspHashes(fileURLToPath(dir));
        logger.info(`${hashes.size} inline script hash(es) written to dist/_headers`);
      },
    },
  };
}
