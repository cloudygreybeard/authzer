(() => {
  const clean = (s) => (s || '').replace(/[\ue000-\uf8ff]/g, '').replace(/[\s\u00a0\ufeff]+/g, ' ').trim();
  const sel = 'button, [role="button"]';
  const el = Array.from(document.querySelectorAll(sel))
    .find(b => {
      const t = clean(b.textContent);
      return t === 'Cancel' || t === 'Close'
        || b.getAttribute('aria-label') === 'Close';
    });
  if (!el) return '';
  const r = el.getBoundingClientRect();
  return JSON.stringify({x: Math.round(r.left + r.width/2),
                         y: Math.round(r.top + r.height/2),
                         tag: el.tagName.toLowerCase(),
                         role: el.getAttribute('role') || '',
                         w: Math.round(r.width),
                         h: Math.round(r.height)});
})()
