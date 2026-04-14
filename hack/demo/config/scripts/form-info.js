(() => {
  const clean = (s) => (s || '').replace(/[\ue000-\uf8ff]/g, '').replace(/[\s\u00a0\ufeff]+/g, ' ').trim();

  const combobox = document.querySelector('[role="combobox"]');
  const account = clean(combobox?.value) || clean(combobox?.textContent);

  const options = Array.from(document.querySelectorAll('[role="option"]'))
    .map(o => clean(o.textContent)).filter(Boolean);
  const accountOptions = options.length > 0 ? options : (account ? [account] : []);

  const roles = [];
  for (const r of document.querySelectorAll('[role="radio"]')) {
    roles.push({
      name: clean(r.textContent) || r.getAttribute('aria-label') || '',
      selected: r.getAttribute('aria-checked') === 'true',
    });
  }
  if (roles.length === 0) {
    for (const r of document.querySelectorAll('input[type="radio"]')) {
      const label = clean(r.closest('label')?.textContent)
        || clean(r.parentElement?.textContent);
      roles.push({ name: label, selected: r.checked });
    }
  }

  let hasTermsCheckbox = false;
  let termsCheckboxLabel = null;
  for (const el of document.querySelectorAll('[role="checkbox"], input[type="checkbox"]')) {
    const label = clean(el.textContent)
      || clean(el.closest('label')?.textContent)
      || clean(el.parentElement?.textContent);
    if (/terms|conditions/i.test(label)) {
      hasTermsCheckbox = true;
      termsCheckboxLabel = label;
      break;
    }
  }

  let termsText = null;
  const dialogEl = document.querySelector('[role="dialog"]')
    || (() => {
      const h2 = Array.from(document.querySelectorAll('h2'))
        .find(h => h.textContent.includes('Request Membership'));
      return h2?.closest('[class*="panel"], [class*="dialog"], [class*="drawer"], [class*="modal"]');
    })()
    || document.body;
  const fullText = dialogEl.innerText;
  const tcMatch = fullText.match(/Terms and Conditions\n([\s\S]*?)(?=\n\s*I have read|$)/);
  if (tcMatch) {
    const t = tcMatch[1].replace(/[\ue000-\uf8ff]/g, '').replace(/[ \t\u00a0]+/g, ' ').replace(/\n{2,}/g, '\n').trim();
    if (t.length > 0 && !/^There (is|are) no /i.test(t)) termsText = t;
  }

  const justField = document.querySelector(
    'textarea[aria-label*="Justification"], textarea[placeholder*="Justification"]');
  const hasJustificationField = !!justField || roles.length > 0;

  return JSON.stringify({
    account, accountOptions, roles,
    hasTermsCheckbox,
    termsCheckboxLabel,
    termsText,
    hasJustificationField,
  });
})()
