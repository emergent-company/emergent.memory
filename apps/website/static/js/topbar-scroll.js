document.addEventListener('DOMContentLoaded', () => {
  const topbar = document.querySelector('[data-at-top]');
  if (topbar) {
    const updateTopbarState = () => {
      const isAtTop = window.scrollY < 30;
      topbar.setAttribute('data-at-top', isAtTop);
    };

    updateTopbarState();
    window.addEventListener('scroll', updateTopbarState, { passive: true });
  }
});
