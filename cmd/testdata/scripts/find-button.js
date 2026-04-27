(() => {
  const sel = 'button, a, [role="button"], [role="link"]';
  const matches = Array.from(document.querySelectorAll(sel))
    .filter(b => {
      if (!b.textContent.includes('%s')) return false;
      const r = b.getBoundingClientRect();
      return r.width > 0 && r.height > 0;
    });
  const el = matches[matches.length - 1];
  if (!el) return '';
  const r = el.getBoundingClientRect();
  return JSON.stringify({x: Math.round(r.left + r.width/2),
                         y: Math.round(r.top + r.height/2),
                         tag: el.tagName.toLowerCase(),
                         role: el.getAttribute('role') || '',
                         w: Math.round(r.width),
                         h: Math.round(r.height)});
})()
