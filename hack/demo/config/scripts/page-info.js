(() => {
  const clean = (s) => (s || '').replace(/[\ue000-\uf8ff]/g, '').replace(/[\s\u00a0\ufeff]+/g, ' ').trim();
  const main = document.querySelector('main') || document.body;
  const text = main.innerText;
  const name = clean((document.querySelector('h1') || {}).textContent);

  const grab = (re) => { const m = text.match(re); return clean(m?.[1]); };

  const id = grab(/\bId\s*:\s*(\S+)/);
  const status = grab(/\bStatus\s*:\s*(\S+)/);
  const domainsRaw = grab(/Domain\(s\)\s*:\s*(.+)/);
  const domains = domainsRaw ? domainsRaw.split(/\s*,\s*/) : [];

  const section = (startRe, endRe) => {
    const combined = new RegExp(startRe.source + '\\n([\\s\\S]*?)' + endRe.source);
    const m = text.match(combined);
    return m?.[1]?.trim() || '';
  };
  const cleanBlock = (s) =>
    s.split('\n').map(l => l.replace(/[\ue000-\uf8ff]/g, '').replace(/[ \t\u00a0]+/g, ' ').trim()).filter(Boolean).join('\n');

  const description = cleanBlock(section(/Description/, /Primary Owner/));
  const splitLines = (s) => s ? s.split('\n').map(l => clean(l)).filter(Boolean) : [];
  const primaryOwners = splitLines(section(/Primary Owner\(s\)/, /Secondary Owner/));
  const secondaryOwners = splitLines(section(/Secondary Owner\(s\)[^\n]*/, /Custom Justification/));

  const justRaw = section(/Custom Justification/, /Terms and Conditions/);
  const customJustification = justRaw.match(/^There (is|are) no /i) ? null : (cleanBlock(justRaw) || null);

  const termsRaw = (text.match(/Terms and Conditions\n([\s\S]*)$/)?.[1] || '').trim();
  const termsAndConditions = termsRaw.match(/^There (is|are) no /i) ? null : (cleanBlock(termsRaw) || null);

  return JSON.stringify({
    name, id, status, domains, description,
    primaryOwners,
    secondaryOwners,
    customJustification,
    termsAndConditions,
  });
})()
