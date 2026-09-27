// Loaded synchronously in <head> so the page paints in the right theme.
// wattop-dark is the default; a stored choice wins.
(function () {
  var t = 'dark';
  try {
    if (localStorage.getItem('wattop-theme') === 'light') t = 'light';
  } catch (e) {}
  document.documentElement.dataset.theme = t;
})();
