(() => {
  const rows = document.querySelectorAll(
    'tbody[role="rowgroup"] tr[role="row"]');
  const items = [];
  for (const row of rows) {
    const cells = row.querySelectorAll('td[role="gridcell"]');
    const nameLink = row.querySelector('a[href*="resource/"]');
    const nameText = nameLink ? nameLink.textContent.trim() : '';
    const href = nameLink ? nameLink.getAttribute('href') : '';
    const slug = href ? href.split('/').pop() : '';
    const account = cells[3] ? cells[3].textContent.trim() : '';
    const role = cells[4] ? cells[4].textContent.trim() : '';
    const dateCell = cells[5];
    const dateText = dateCell ? dateCell.textContent.trim() : '';
    const expiring = dateCell
      ? !!dateCell.querySelector('.expiring-warning')
      : false;
    items.push({
      name: nameText,
      id: slug,
      account: account,
      role: role,
      expirationDate: dateText,
      expiring: expiring
    });
  }
  return JSON.stringify(items);
})()
