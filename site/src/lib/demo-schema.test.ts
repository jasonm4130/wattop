// Ties the landing-page dashboard to the product so a schema change fails CI:
//  1. every field src/lib/dash.ts reads exists in the committed demo data,
//     with the type dash.ts expects;
//  2. every raw `--json` field scripts/demo-schema.mjs reads is still a JSON
//     tag in internal/domain/*.go (a rename there fails here).
import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import demo from '../data/demo.json';
import { SOURCE_FIELDS } from '../../scripts/demo-schema.mjs';
import { renderDash, WINDOW } from './dash';
import type { Demo } from './dash';

type Spec =
  | 'number'
  | 'string'
  | 'boolean'
  | 'number?' // number or null
  | 'string?' // string, null or absent
  | 'boolean?' // boolean or absent
  | Spec[] // tuple
  | { array: Spec }
  | { [k: string]: Spec };

const child: Spec = {
  k: 'string',
  st: 'string',
  out: 'number?',
  cost: 'number?',
  burn: 'number?',
};
const subagent: Spec = { ...child, type: 'string', desc: 'string', model: 'string?', tool: 'string?', depth: 'number', bg: 'boolean' };
const workflow: Spec = { ...child, id: 'string', phase: 'string', agents: 'number', running: 'number', done: 'number', failed: 'number', partial: 'boolean' };

// Mirrors the Frame / Session / Child types in src/lib/dash.ts.
const frameSpec: Spec = {
  out: 'number',
  in: 'number',
  cpu: 'number',
  sys: {
    e: ['number', 'number', 'number'],
    p: ['number', 'number', 'number'],
    gpu: ['number', 'number', 'number'],
    w: { sys: 'number', cpu: 'number', gpu: 'number', ane: 'number', dram: 'number' },
    t: ['number', 'number', 'number'],
    fans: { array: ['string', 'number'] },
    thermal: 'number',
    throttled: 'boolean',
    mem: ['number', 'number', 'number', 'number'],
    dram: 'number?',
    dramEst: 'boolean',
    net: ['number', 'number'],
    disk: ['number', 'number'],
  },
  total: { cost: 'number', burn: 'number', self: 'number' },
  sessions: {
    array: {
      agent: 'string',
      kind: 'string',
      pid: 'number?',
      bind: 'string',
      cwd: 'string',
      model: 'string',
      st: 'string',
      ctx: 'number',
      exact: 'boolean',
      out: 'number?',
      cost: 'number?',
      partial: 'boolean',
      burn: 'number?',
      tl: 'number',
      sa: ['number', 'number'],
      cpu: 'number?',
      gpu: 'number?',
      rss: 'number?',
      children: { array: 'CHILD' as unknown as Spec },
    },
  },
};

function check(v: unknown, spec: Spec, path: string, errs: string[]): void {
  if (typeof spec === 'string') {
    if ((spec as string) === 'CHILD') {
      const c = v as { k?: string };
      return check(v, c?.k === 'wf' ? workflow : subagent, path, errs);
    }
    const ok =
      spec === 'number' ? typeof v === 'number' && Number.isFinite(v)
      : spec === 'string' ? typeof v === 'string'
      : spec === 'boolean' ? typeof v === 'boolean'
      : spec === 'number?' ? v === null || (typeof v === 'number' && Number.isFinite(v))
      : spec === 'string?' ? v == null || typeof v === 'string'
      : v === undefined || typeof v === 'boolean';
    if (!ok) errs.push(`${path}: expected ${spec}, got ${JSON.stringify(v)}`);
    return;
  }
  if (Array.isArray(spec)) {
    if (!Array.isArray(v) || v.length !== spec.length) {
      errs.push(`${path}: expected ${spec.length}-tuple, got ${JSON.stringify(v)}`);
      return;
    }
    spec.forEach((s, i) => check(v[i], s, `${path}[${i}]`, errs));
    return;
  }
  if ('array' in spec && Object.keys(spec).length === 1) {
    if (!Array.isArray(v)) {
      errs.push(`${path}: expected array`);
      return;
    }
    v.forEach((x, i) => check(x, spec.array as Spec, `${path}[${i}]`, errs));
    return;
  }
  if (v === null || typeof v !== 'object') {
    errs.push(`${path}: expected object`);
    return;
  }
  for (const [k, s] of Object.entries(spec)) check((v as Record<string, unknown>)[k], s, `${path}.${k}`, errs);
}

describe('committed demo data (src/data/demo.json)', () => {
  const d = demo as unknown as Demo;

  it('is labelled synthetic and has enough frames to loop', () => {
    expect(d.meta.synthetic).toBe(true);
    expect(typeof d.meta.soc).toBe('string');
    expect(d.frames.length).toBeGreaterThanOrEqual(2);
  });

  it('has every field dash.ts reads, with the right type', () => {
    const errs: string[] = [];
    d.frames.forEach((f, i) => check(f, frameSpec, `frames[${i}]`, errs));
    expect(errs.slice(0, 20)).toEqual([]);
  });

  it('exercises sessions, subagents and a workflow', () => {
    const kids = d.frames[0].sessions.flatMap((s) => s.children);
    expect(d.frames[0].sessions.length).toBeGreaterThan(0);
    expect(kids.some((c) => c.k === 'sa')).toBe(true);
    expect(kids.some((c) => c.k === 'wf')).toBe(true);
  });

  it('renders every frame the way the TUI labels rows', () => {
    for (let t = WINDOW - 1; t < WINDOW - 1 + d.frames.length; t++) {
      const html = renderDash(d.frames, t);
      expect(html).toContain('⠋</span> Busy');
      expect(html).not.toMatch(/[├└]/);
      expect(html).not.toContain('undefined');
      expect(html).not.toContain('NaN');
    }
  });
});

describe('demo trimmer vs the Go --json schema', () => {
  const domain = fileURLToPath(new URL('../../../internal/domain/', import.meta.url));
  const tags = new Set<string>();
  for (const name of readdirSync(domain)) {
    if (!name.endsWith('.go') || name.endsWith('_test.go')) continue;
    for (const m of readFileSync(join(domain, name), 'utf8').matchAll(/json:"([a-z0-9_]+)/g)) tags.add(m[1]);
  }

  it('found the domain package tags', () => {
    expect(tags.size).toBeGreaterThan(50);
    expect(tags.has('sessions')).toBe(true);
  });

  it.each(SOURCE_FIELDS as string[])('%s is a JSON tag path in internal/domain', (path) => {
    // "{}" introduces a Go map key (sys.temps{}cpu); only the part before it is tags.
    const [tagged] = path.split('{}');
    const missing = tagged
      .split('.')
      .map((seg) => seg.replace(/\[\]$/, ''))
      .filter((seg) => !tags.has(seg));
    expect(missing).toEqual([]);
  });
});
