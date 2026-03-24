(() => {
  const ta = document.querySelector(
    '[role="dialog"] textarea, ' +
    'textarea[placeholder*="Justification"]');
  if (!ta) return false;
  ta.focus();
  ta.value = '';
  document.execCommand('insertText', false, %s);
  return ta.value.length > 0;
})()
