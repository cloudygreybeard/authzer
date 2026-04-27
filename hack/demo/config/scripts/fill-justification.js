(() => {
  const d = document.querySelector('[role="dialog"]')
    || document.querySelector('.ms-Dialog-main');

  const ta = d
    ? d.querySelector('textarea')
    : document.querySelector('textarea[placeholder*="Justification"]');
  if (ta) {
    ta.focus();
    ta.value = '';
    document.execCommand('insertText', false, %s);
    return ta.value.length > 0;
  }

  const scope = d || document;
  const radio = scope.querySelector('[role="radio"][aria-checked="false"]')
    || scope.querySelector('input[type="radio"]:not(:checked)');
  if (radio) {
    radio.click();
    return true;
  }

  return false;
})()
