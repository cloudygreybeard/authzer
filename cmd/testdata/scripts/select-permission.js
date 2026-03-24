(() => {
  const target = %s;
  for (const r of document.querySelectorAll('[role="radio"]')) {
    if (r.textContent.trim() === target) {
      r.click();
      return true;
    }
  }
  for (const r of document.querySelectorAll('input[type="radio"]')) {
    const label = r.closest('label')?.textContent.trim()
      || r.parentElement?.textContent.trim() || '';
    if (label === target) {
      r.click();
      return true;
    }
  }
  return false;
})()
