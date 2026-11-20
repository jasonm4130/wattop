(function () {
  var t = 'dark';
  try {
    if (localStorage.getItem('wattop-theme') === 'light') t = 'light';
  } catch (e) {}
  document.documentElement.dataset.theme = t;
})();
