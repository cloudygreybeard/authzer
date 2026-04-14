(() => {
  const clean = (s) => (s || '').replace(/[\ue000-\uf8ff]/g, '').replace(/[\s\u00a0\ufeff]+/g, ' ').trim();
  for (const el of document.querySelectorAll(
    '[role="checkbox"], input[type="checkbox"]')) {
    const label = clean(el.textContent)
      || clean(el.closest('label')?.textContent)
      || clean(el.parentElement?.textContent);
    if (/terms|conditions/i.test(label)) {
      if (el.getAttribute('aria-checked') !== 'true' && !el.checked) {
        el.click();
      }
      return true;
    }
  }
  return false;
})()
