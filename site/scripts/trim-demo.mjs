import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { trimFrame } from './demo-schema.mjs';

const src = process.argv[2];
if (!src) {
  console.error('usage: node scripts/trim-demo.mjs <demo-frames.ndjson>');
  process.exit(1);
}
const here = dirname(fileURLToPath(import.meta.url));
const out = resolve(here, '../src/data/demo.json');

const frames = readFileSync(src, 'utf8')
  .split('\n')
  .filter(Boolean)
  .map((l) => JSON.parse(l));
if (frames.length < 2) {
  console.error(`trim-demo: need at least 2 frames, got ${frames.length}`);
  process.exit(1);
}

const version = process.env.WATTOP_VERSION || 'v0.3.0';
const meta = {
  source: `wattop --demo --json (${version}), ${frames.length} consecutive snapshots at 1s`,
  soc: frames[0].sys.soc_name,
  synthetic: true,
};

mkdirSync(dirname(out), { recursive: true });
const json = JSON.stringify({ meta, frames: frames.map(trimFrame) });
writeFileSync(out, json);
console.log(`wrote ${out} (${(json.length / 1024).toFixed(1)} KB, ${frames.length} frames)`);
