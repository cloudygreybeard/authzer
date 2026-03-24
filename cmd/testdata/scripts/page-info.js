(() => {
  const main = document.querySelector('main') || document.body;
  const text = main.innerText;
  const name = (document.querySelector('h1') || {}).textContent?.trim() || '';

  const grab = (re) => { const m = text.match(re); return m?.[1]?.trim() || ''; };

  const id = grab(/\bId\s*:\s*(\S+)/);
  const status = grab(/\bStatus\s*:\s*(\S+)/);
  const domainsRaw = grab(/Domain\(s\)\s*:\s*(.+)/);
  const domains = domainsRaw ? domainsRaw.split(/\s*,\s*/) : [];

  const section = (startRe, endRe) => {
    const combined = new RegExp(startRe.source + '\\n([\\s\\S]*?)' + endRe.source);
    const m = text.match(combined);
    return m?.[1]?.trim() || '';
  };

  const description = section(/Description/, /Primary Owner/);
  const splitLines = (s) => s ? s.split('\n').map(l => l.trim()).filter(Boolean) : [];
  const primaryOwners = splitLines(section(/Primary Owner\(s\)/, /Secondary Owner/));
  const secondaryOwners = splitLines(section(/Secondary Owner\(s\)[^\n]*/, /Custom Justification/));

  const justRaw = section(/Custom Justification/, /Terms and Conditions/);
  const customJustification = justRaw.match(/^There (is|are) no /i) ? null : (justRaw || null);

  const termsRaw = (text.match(/Terms and Conditions\n([\s\S]*)$/)?.[1] || '').trim();
  const termsAndConditions = termsRaw.match(/^There (is|are) no /i) ? null : (termsRaw || null);

  return JSON.stringify({
    name, id, status, domains, description,
    primaryOwners,
    secondaryOwners,
    customJustification,
    termsAndConditions,
  });
})()
