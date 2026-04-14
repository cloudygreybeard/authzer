(() => {
  const clean = (s) => (s || '').replace(/[\ue000-\uf8ff]/g, '').replace(/[\s\u00a0\ufeff]+/g, ' ').trim();
  const rows = document.querySelectorAll(
    'tbody[role="rowgroup"] tr[role="row"]');
  const items = [];
  for (const row of rows) {
    const cells = row.querySelectorAll('td[role="gridcell"]');
    const nameLink = row.querySelector('a[href*="access/"]');
    const nameText = nameLink ? clean(nameLink.textContent) : '';
    const href = nameLink ? nameLink.getAttribute('href') : '';
    const slug = href ? href.split('/').pop().trim() : '';
    const account = cells[3] ? clean(cells[3].textContent) : '';
    const role = cells[4] ? clean(cells[4].textContent) : '';
    const dateCell = cells[5];
    const dateText = dateCell ? clean(dateCell.textContent) : '';
    const expiring = dateCell
      ? !!dateCell.querySelector('.expiry-warning')
      : false;
    items.push({
      name: nameText,
      id: slug,
      selfLink: href ? (location.origin + (href.startsWith('/') ? '' : '/') + href) : '',
      account: account,
      role: role,
      expirationDate: dateText,
      expiring: expiring
    });
  }
  return JSON.stringify(items);
})()
