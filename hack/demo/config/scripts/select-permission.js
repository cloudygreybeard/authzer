(() => {
  const clean = (s) => (s || '').replace(/[\ue000-\uf8ff]/g, '').replace(/[\s\u00a0\ufeff]+/g, ' ').trim();
  const target = %s;
  for (const r of document.querySelectorAll('[role="radio"]')) {
    if (clean(r.textContent) === target) {
      r.click();
      return true;
    }
  }
  for (const r of document.querySelectorAll('input[type="radio"]')) {
    const label = clean(r.closest('label')?.textContent)
      || clean(r.parentElement?.textContent);
    if (label === target) {
      r.click();
      return true;
    }
  }
  return false;
})()
