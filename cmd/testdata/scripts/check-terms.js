(() => {
  for (const el of document.querySelectorAll(
    '[role="checkbox"], input[type="checkbox"]')) {
    const label = el.textContent.trim()
      || el.closest('label')?.textContent.trim()
      || el.parentElement?.textContent.trim() || '';
    if (/terms|conditions/i.test(label)) {
      if (el.getAttribute('aria-checked') !== 'true' && !el.checked) {
        el.click();
      }
      return true;
    }
  }
  return false;
})()
