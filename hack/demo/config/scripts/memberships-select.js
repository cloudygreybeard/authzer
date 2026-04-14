(() => {
  const target = "%s";
  const normalize = s => s.replace(/[\s\u00a0\ufeff]+/g, ' ').trim();
  const normalized = normalize(target);

  let cb = document.querySelector('input.row-select[aria-label="' + target + '"]');

  if (!cb) {
    const all = document.querySelectorAll('input.row-select[aria-label]');
    for (const el of all) {
      if (normalize(el.getAttribute('aria-label')) === normalized) {
        cb = el;
        break;
      }
    }
  }

  if (!cb) return false;
  cb.scrollIntoView({ block: 'center', behavior: 'instant' });
  cb.click();
  return true;
})()
