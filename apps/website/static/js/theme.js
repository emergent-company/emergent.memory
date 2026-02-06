const savedTheme = localStorage.getItem('theme');
const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
const defaultTheme =
  savedTheme ||
  (prefersDark ? 'space-asteroid-belt' : 'space-asteroid-belt-light');

document.documentElement.setAttribute('data-theme', defaultTheme);

document.addEventListener('DOMContentLoaded', () => {
  document.querySelectorAll('.theme-option').forEach((btn) => {
    btn.addEventListener('click', () => {
      const newTheme = btn.getAttribute('data-theme');
      document.documentElement.setAttribute('data-theme', newTheme);
      localStorage.setItem('theme', newTheme);
    });
  });
});
