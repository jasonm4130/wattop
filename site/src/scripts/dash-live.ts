// Animates the server-rendered dashboard: one recorded frame per second.
// Frozen (no timer at all) under prefers-reduced-motion; the pause button
// mirrors the TUI's `p` (pause the display).
import { renderDash, WINDOW } from '../lib/dash';
import type { Demo } from '../lib/dash';

const root = document.querySelector<HTMLElement>('[data-dash]');
const btn = document.querySelector<HTMLButtonElement>('[data-dash-pause]');

if (root) {
  const reduce = matchMedia('(prefers-reduced-motion: reduce)');
  let tick = Number(root.dataset.tick || WINDOW - 1);
  let timer: number | undefined;
  let demo: Demo | undefined;
  let paused = reduce.matches;

  const label = () => {
    if (!btn) return;
    btn.setAttribute('aria-pressed', String(paused));
    btn.querySelector('[data-l]')!.textContent = paused ? 'play' : 'pause';
  };

  const step = () => {
    if (!demo || document.hidden) return;
    tick += 1;
    root.innerHTML = renderDash(demo.frames, tick);
  };

  const start = async () => {
    if (!demo) demo = (await import('../data/demo.json')).default as unknown as Demo;
    if (timer === undefined && !paused) timer = window.setInterval(step, 1000);
  };
  const stop = () => {
    if (timer !== undefined) window.clearInterval(timer);
    timer = undefined;
  };

  btn?.addEventListener('click', () => {
    paused = !paused;
    label();
    if (paused) stop();
    else start();
  });
  reduce.addEventListener('change', () => {
    paused = reduce.matches;
    label();
    if (paused) stop();
    else start();
  });

  // Start only once the hero is on screen, and stop when it leaves.
  const io = new IntersectionObserver((entries) => {
    for (const e of entries) {
      if (e.isIntersecting && !paused) start();
      else if (!e.isIntersecting) stop();
    }
  });
  io.observe(root);
  label();
}
