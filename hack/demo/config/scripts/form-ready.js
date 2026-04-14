(() => {
  const d = document.querySelector('[role="dialog"]');
  if (!d) return false;
  return !!(d.querySelector('[role="combobox"]')
    || d.querySelector('[role="radio"]')
    || d.querySelector('input[type="radio"]')
    || d.querySelector('textarea')
    || d.querySelector('[role="checkbox"]')
    || d.querySelector('input[type="checkbox"]'));
})()
