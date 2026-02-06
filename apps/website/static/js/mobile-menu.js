document.addEventListener('DOMContentLoaded', () => {
  const drawer = document.getElementById('mobile-drawer');
  const links = document.querySelectorAll('.drawer-side a');

  links.forEach((link) => {
    link.addEventListener('click', () => {
      if (drawer) {
        drawer.checked = false;
      }
    });
  });
});
