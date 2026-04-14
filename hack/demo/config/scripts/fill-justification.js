(() => {
  const d = document.querySelector('[role="dialog"]');

  const ta = d
    ? d.querySelector('textarea')
    : document.querySelector('textarea[placeholder*="Justification"]');
  if (ta) {
    ta.focus();
    ta.value = '';
    document.execCommand('insertText', false, %s);
    return ta.value.length > 0;
  }

  if (!d) return false;

  const radio = d.querySelector('[role="radio"][aria-checked="false"]')
    || d.querySelector('input[type="radio"]:not(:checked)');
  if (radio) {
    radio.click();
    return true;
  }

  return false;
})()
