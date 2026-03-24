(() => {
  const cb = document.querySelector('input[type="checkbox"][aria-label="%s"]');
  if (!cb) return false;
  cb.click();
  return true;
})()
