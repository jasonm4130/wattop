const KEY = 'wattop-theme';

function current(): 'dark' | 'light' {
  return document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
}

function paint() {
  const t = current();
  document.querySelectorAll<HTMLElement>('[data-theme-name]').forEach((el) => {
    el.textContent = `wattop-${t}`;
  });
  document.querySelectorAll<HTMLButtonElement>('[data-theme-toggle]').forEach((b) => {
    b.setAttribute('aria-label', `Colour theme: wattop-${t}. Switch to wattop-${t === 'dark' ? 'light' : 'dark'}`);
  });
}

function set(t: 'dark' | 'light') {
  document.documentElement.dataset.theme = t;
  try {
    localStorage.setItem(KEY, t);
  } catch {
    /* storage unavailable: the choice lasts for this page only */
  }
  paint();
}

document.querySelectorAll<HTMLButtonElement>('[data-theme-toggle]').forEach((b) => {
  b.addEventListener('click', () => set(current() === 'dark' ? 'light' : 'dark'));
});
paint();
