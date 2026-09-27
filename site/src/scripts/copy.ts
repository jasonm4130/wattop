// Copy-to-clipboard for install commands.
document.querySelectorAll<HTMLButtonElement>('[data-copy]').forEach((b) => {
  b.addEventListener('click', async () => {
    const text = b.dataset.copy || '';
    const label = b.querySelector('[data-copy-l]');
    try {
      await navigator.clipboard.writeText(text);
      if (label) label.textContent = 'copied';
    } catch {
      if (label) label.textContent = 'select + ⌘C';
    }
    window.setTimeout(() => {
      if (label) label.textContent = 'copy';
    }, 1600);
  });
});
