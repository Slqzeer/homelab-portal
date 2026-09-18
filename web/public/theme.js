(() => {
  const root = document.documentElement;
  const media = window.matchMedia('(prefers-color-scheme: dark)');
  let preference = null;

  try {
    const stored = window.localStorage.getItem('portal.theme');
    if (stored === 'light' || stored === 'dark') {
      preference = stored;
    }
  } catch {
    // Storage is optional; the operating-system preference remains safe.
  }

  root.dataset.theme = preference ?? (media.matches ? 'dark' : 'light');
})();
