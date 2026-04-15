// Copyright 2026 cloudygreybeard
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"
)

func membershipsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var rows strings.Builder
	for _, e := range entitlements {
		expiry := e.Expiry.Format("January 02, 2006 03:04 PM UTC")
		warningClass := ""
		if time.Until(e.Expiry).Hours()/24 < 30 {
			warningClass = ` class="expiry-warning"`
		}

		fmt.Fprintf(&rows, `<tr role="row">
  <td role="gridcell"><input class="row-select" type="checkbox" aria-label="%s"></td>
  <td role="gridcell"><a href="/portal/access/%s">%s</a></td>
  <td role="gridcell">Active</td>
  <td role="gridcell">%s</td>
  <td role="gridcell">%s</td>
  <td role="gridcell"><span%s>%s</span></td>
</tr>
`, html.EscapeString(e.Name), e.ID, html.EscapeString(e.Name), html.EscapeString(e.Account),
			html.EscapeString(e.Role), warningClass, expiry)
	}

	var termsMap strings.Builder
	for _, e := range entitlements {
		if e.HasTerms {
			fmt.Fprintf(&termsMap, "    %q: %q,\n", e.ID, e.TermsText)
		}
	}

	page := strings.Replace(membershipsHTML, "TERMS_MAP_PLACEHOLDER", termsMap.String(), 1)
	fmt.Fprintf(w, page, rows.String())
}

func detailPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	parts := strings.Split(r.URL.Path, "/")
	id := parts[len(parts)-1]

	e := findEntitlement(id)
	if e == nil {
		http.NotFound(w, r)
		return
	}

	expiry := e.Expiry.Format("January 02, 2006 03:04 PM UTC")
	termsBlock := "There are no terms and conditions."
	if e.HasTerms {
		termsBlock = html.EscapeString(e.TermsText)
	}

	fmt.Fprintf(w, detailHTML,
		html.EscapeString(e.Name),
		html.EscapeString(e.Name),
		html.EscapeString(e.ID),
		html.EscapeString(e.Account),
		html.EscapeString(e.Role),
		expiry,
		html.EscapeString(e.Description),
		termsBlock,
	)
}

var membershipsHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Access Portal - Entitlements</title>
<style>
  body { font-family: system-ui, -apple-system, sans-serif; margin: 0; padding: 0; background: #f5f5f4; color: #1c1917; }
  .header { background: #2563eb; color: #fff; padding: 12px 24px; }
  .header .title { margin: 0; font-size: 20px; font-weight: 600; }
  .content { max-width: 1200px; margin: 24px auto; padding: 0 24px; }
  .toolbar { display: flex; gap: 8px; margin-bottom: 16px; }
  .toolbar button { padding: 6px 16px; border: 1px solid #a8a29e; background: #fff; cursor: pointer; font-size: 14px; }
  .toolbar button:hover { background: #e7e5e4; }
  .toolbar button:disabled { opacity: 0.4; cursor: default; }
  table { width: 100%%; border-collapse: collapse; background: #fff; }
  th { text-align: left; padding: 8px 12px; background: #fafaf9; border-bottom: 1px solid #e7e5e4; font-weight: 600; font-size: 13px; color: #78716c; }
  td { padding: 8px 12px; border-bottom: 1px solid #f5f5f4; font-size: 14px; }
  td a { color: #2563eb; text-decoration: none; }
  td a:hover { text-decoration: underline; }
  .row-select { width: 16px; height: 16px; cursor: pointer; }
  .expiry-warning { color: #dc2626; font-weight: 600; }

  .dialog-overlay { display: none; position: fixed; top: 0; left: 0; width: 100%%; height: 100%%; background: rgba(0,0,0,0.4); z-index: 1000; justify-content: center; align-items: center; }
  .dialog-overlay.active { display: flex; }
  .dialog-box { background: #fff; border-radius: 4px; padding: 24px; width: 560px; max-height: 80vh; overflow-y: auto; box-shadow: 0 2px 8px rgba(0,0,0,0.26); }
  .dialog-box h2 { margin: 0 0 16px; font-size: 20px; }
  .form-group { margin-bottom: 16px; }
  .form-group label { display: block; margin-bottom: 4px; font-weight: 600; font-size: 14px; }
  .form-group textarea { width: 100%%; min-height: 80px; padding: 8px; border: 1px solid #a8a29e; border-radius: 2px; font-family: inherit; font-size: 14px; box-sizing: border-box; }
  .radio-group { display: flex; flex-direction: column; gap: 8px; margin-top: 4px; }
  .radio-item { display: flex; align-items: center; gap: 8px; padding: 8px; border: 1px solid #e7e5e4; border-radius: 4px; cursor: pointer; }
  .radio-item:hover { background: #f5f5f4; }
  .checkbox-group { margin-top: 8px; display: flex; align-items: flex-start; gap: 8px; }
  .checkbox-group input { margin-top: 4px; }
  .terms-text { font-size: 13px; color: #78716c; margin-top: 8px; padding: 12px; background: #fafaf9; border-radius: 4px; line-height: 1.5; }
  .combobox-field { padding: 6px 12px; border: 1px solid #a8a29e; border-radius: 2px; font-size: 14px; width: 100%%; box-sizing: border-box; background: #fff; }
  .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 24px; }
  .dialog-actions button { padding: 6px 20px; font-size: 14px; border-radius: 2px; cursor: pointer; }
  .btn-primary { background: #2563eb; color: #fff; border: none; }
  .btn-primary:hover { background: #1d4ed8; }
  .btn-secondary { background: #fff; border: 1px solid #a8a29e; }
  .btn-secondary:hover { background: #e7e5e4; }
  .success-banner { display: none; background: #dcfce7; border: 1px solid #16a34a; padding: 12px 24px; margin-bottom: 16px; border-radius: 4px; color: #16a34a; font-weight: 600; }
  .success-banner.active { display: block; }
</style>
</head>
<body>
<div class="header"><div class="title">Access Portal</div></div>
<div class="content">
  <div id="successBanner" class="success-banner">Membership renewed successfully.</div>
  <div class="toolbar">
    <button id="renewBtn" onclick="openRenewDialog()">Renew</button>
    <button onclick="openRequestDialog()">Request Membership</button>
  </div>
  <table role="grid">
    <thead>
      <tr>
        <th></th>
        <th>Name</th>
        <th>Status</th>
        <th>Account</th>
        <th>Role</th>
        <th>Expiration Date</th>
      </tr>
    </thead>
    <tbody role="rowgroup">
      %s
    </tbody>
  </table>
</div>

<div id="dialogOverlay" class="dialog-overlay" role="dialog" aria-label="Renew Membership">
  <div class="dialog-box">
    <h2 id="dialogTitle">Renew Membership</h2>
    <div class="form-group">
      <label>Account</label>
      <div role="combobox" aria-expanded="false" class="combobox-field" id="accountCombo">demo/user</div>
    </div>
    <div class="form-group">
      <label>Permission</label>
      <div class="radio-group" id="permissionRadios">
        <div class="radio-item" role="radio" aria-checked="true" tabindex="0" onclick="selectRadio(this)">ReadOnly</div>
        <div class="radio-item" role="radio" aria-checked="false" tabindex="0" onclick="selectRadio(this)">ReadWrite</div>
      </div>
    </div>
    <div class="form-group">
      <label>Justification</label>
      <textarea placeholder="Justification" aria-label="Justification"></textarea>
    </div>
    <div id="termsGroup" class="form-group" style="display:none;">
      <div class="checkbox-group">
        <input type="checkbox" id="termsCheck" role="checkbox" aria-checked="false">
        <label for="termsCheck">I accept the terms and conditions</label>
      </div>
      <div id="termsTextBlock" class="terms-text">
        <strong>Terms and Conditions</strong><br>
        <span id="termsContent"></span>
      </div>
    </div>
    <div class="dialog-actions">
      <button class="btn-secondary" role="button" onclick="closeDialog()">Cancel</button>
      <button class="btn-primary" role="button" id="submitBtn" onclick="submitRenew()">Renew</button>
    </div>
  </div>
</div>

<div id="requestDialogOverlay" class="dialog-overlay" role="dialog" aria-label="Request Membership">
  <div class="dialog-box">
    <h2>Request Membership</h2>
    <div class="form-group">
      <label>Request type</label>
      <div class="radio-group">
        <div class="radio-item" role="radio" aria-checked="false" tabindex="0" onclick="selectRadio(this); document.getElementById('requestDialogOverlay').classList.remove('active'); openRenewDialog();">This Account</div>
        <div class="radio-item" role="radio" aria-checked="false" tabindex="0" onclick="selectRadio(this)">Another Account</div>
      </div>
    </div>
    <div class="dialog-actions">
      <button class="btn-secondary" role="button" onclick="document.getElementById('requestDialogOverlay').classList.remove('active')">Cancel</button>
    </div>
  </div>
</div>

<script>
let selectedEntitlement = null;

document.querySelectorAll('.row-select').forEach(cb => {
  cb.addEventListener('change', function() {
    document.querySelectorAll('.row-select').forEach(o => {
      if (o !== this) o.checked = false;
    });
    selectedEntitlement = this.checked ? this.getAttribute('aria-label') : null;
  });
  cb.addEventListener('click', function() {
    setTimeout(() => {
      selectedEntitlement = this.checked ? this.getAttribute('aria-label') : null;
    }, 0);
  });
});

function selectRadio(el) {
  el.parentElement.querySelectorAll('[role="radio"]').forEach(r => r.setAttribute('aria-checked', 'false'));
  el.setAttribute('aria-checked', 'true');
}

function openRenewDialog() {
  const overlay = document.getElementById('dialogOverlay');
  const ta = overlay.querySelector('textarea');
  ta.value = '';
  const termsCheck = document.getElementById('termsCheck');
  termsCheck.checked = false;
  termsCheck.setAttribute('aria-checked', 'false');

  // Determine if selected entitlement has terms
  const termsGroup = document.getElementById('termsGroup');
  const termsContent = document.getElementById('termsContent');
  const entitlementTerms = getEntitlementTerms(selectedEntitlement);
  if (entitlementTerms) {
    termsGroup.style.display = 'block';
    termsContent.textContent = entitlementTerms;
  } else {
    termsGroup.style.display = 'none';
  }

  termsCheck.addEventListener('click', function() {
    this.setAttribute('aria-checked', this.checked ? 'true' : 'false');
  });

  overlay.classList.add('active');
}

function openRequestDialog() {
  document.getElementById('requestDialogOverlay').classList.add('active');
}

function closeDialog() {
  document.getElementById('dialogOverlay').classList.remove('active');
}

function submitRenew() {
  closeDialog();
  document.getElementById('successBanner').classList.add('active');
  setTimeout(() => document.getElementById('successBanner').classList.remove('active'), 5000);
}

function getEntitlementTerms(id) {
  const termsMap = {
TERMS_MAP_PLACEHOLDER
  };
  return termsMap[id] || null;
}
</script>
</body>
</html>`

var detailHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>%s - Access Portal</title>
<style>
  body { font-family: system-ui, -apple-system, sans-serif; margin: 0; padding: 0; background: #f5f5f4; color: #1c1917; }
  .header { background: #2563eb; color: #fff; padding: 12px 24px; }
  .header .title { margin: 0; font-size: 20px; font-weight: 600; }
  main { max-width: 900px; margin: 24px auto; padding: 24px; background: #fff; border-radius: 4px; box-shadow: 0 1px 3px rgba(0,0,0,0.12); }
  main h1 { font-size: 24px; margin-top: 0; }
  .field { margin-bottom: 12px; }
  .field-label { font-weight: 600; color: #78716c; font-size: 13px; }
  .field-value { font-size: 14px; margin-top: 2px; }
  .section-title { font-size: 16px; font-weight: 600; margin-top: 24px; margin-bottom: 8px; border-bottom: 1px solid #e7e5e4; padding-bottom: 4px; }
  .toolbar { margin-bottom: 16px; }
  .toolbar button { padding: 6px 16px; border: 1px solid #a8a29e; background: #fff; cursor: pointer; font-size: 14px; }
  .toolbar button:hover { background: #e7e5e4; }
  .overlay { display:none; position:fixed; top:0; left:0; width:100%%; height:100%%; background:rgba(0,0,0,0.4); z-index:100; }
  .overlay.open { display:flex; align-items:center; justify-content:center; }
  .dialog { background:#fff; border-radius:4px; padding:24px; width:480px; max-height:80vh; overflow-y:auto; box-shadow:0 4px 12px rgba(0,0,0,0.2); }
  .dialog h2 { margin-top:0; font-size:18px; }
  .dialog label { display:block; margin-bottom:8px; font-size:14px; }
  .dialog textarea { width:100%%; height:60px; margin-top:4px; font-family:inherit; font-size:14px; }
  .dialog .btn-row { display:flex; gap:8px; margin-top:16px; justify-content:flex-end; }
  .dialog .btn-row button { padding:6px 20px; border:1px solid #a8a29e; background:#fff; cursor:pointer; font-size:14px; }
  .dialog .btn-row button.primary { background:#2563eb; color:#fff; border-color:#2563eb; }
</style>
</head>
<body>
<div class="header"><div class="title">Access Portal</div></div>
<main>
  <h1>%s</h1>
  <div class="toolbar">
    <button role="button" id="requestBtn">Request Membership</button>
  </div>
  <div class="field"><span class="field-label">Id: </span><span class="field-value">%s</span></div>
  <div class="field"><span class="field-label">Account: </span><span class="field-value" id="accountVal">%s</span></div>
  <div class="field"><span class="field-label">Role: </span><span class="field-value" id="roleVal">%s</span></div>
  <div class="field"><span class="field-label">Status: </span><span class="field-value">Active</span></div>
  <div class="field"><span class="field-label">Expiration Date: </span><span class="field-value">%s</span></div>
  <div class="field"><span class="field-label">Domain(s): </span><span class="field-value">demo.example.com</span></div>

  <div class="section-title">Description</div>
  <p>%s</p>

  <div class="section-title">Primary Owner(s)</div>
  <p>portal-admins@example.com</p>

  <div class="section-title">Secondary Owner(s)</div>
  <p>security-team@example.com</p>

  <div class="section-title">Custom Justification</div>
  <p>There is no custom justification.</p>

  <div class="section-title">Terms and Conditions</div>
  <p>%s</p>
</main>

<div class="overlay" id="choiceOverlay">
  <div role="dialog" aria-label="Request type" class="dialog">
    <h2>Request Membership</h2>
    <p>Who is this request for?</p>
    <div class="btn-row">
      <button id="forMyselfBtn" role="button">This Account</button>
      <button role="button">Another Account</button>
    </div>
  </div>
</div>

<div class="overlay" id="formOverlay">
  <div role="dialog" aria-label="Request form" class="dialog" id="formDialog">
    <h2>Request Membership</h2>
    <label>Account
      <div role="combobox" aria-expanded="false" id="acctCombo" style="border:1px solid #a8a29e; padding:6px; border-radius:2px; cursor:pointer;"></div>
      <div id="acctOptions" style="display:none;"></div>
    </label>
    <label>Permission Level</label>
    <div id="roleRadios" style="margin-bottom:12px;"></div>
    <div id="termsSection" style="display:none; margin-bottom:12px;">
      <label><input type="checkbox" role="checkbox"> I have read and agree to the Terms and Conditions</label>
    </div>
    <label>Justification
      <textarea aria-label="Justification" placeholder="Justification"></textarea>
    </label>
    <div class="btn-row">
      <button role="button" aria-label="Close" id="closeFormBtn">Cancel</button>
      <button role="button" class="primary" id="submitFormBtn">Submit</button>
    </div>
  </div>
</div>

<script>
(function(){
  var acct = document.getElementById('accountVal').textContent.trim();
  var role = document.getElementById('roleVal').textContent.trim();

  document.getElementById('acctCombo').textContent = acct;
  var optDiv = document.getElementById('acctOptions');
  var opt = document.createElement('div');
  opt.setAttribute('role','option');
  opt.textContent = acct;
  optDiv.appendChild(opt);

  var radios = document.getElementById('roleRadios');
  ['ReadOnly','ReadWrite'].forEach(function(r){
    var d = document.createElement('div');
    d.setAttribute('role','radio');
    d.setAttribute('aria-checked', r===role ? 'true' : 'false');
    d.textContent = r;
    d.style.cssText = 'padding:4px 0; cursor:pointer;';
    radios.appendChild(d);
  });

  var termsP = document.querySelector('main .section-title:last-of-type + p');
  if (termsP && !/^There (is|are) no /i.test(termsP.textContent.trim())) {
    document.getElementById('termsSection').style.display = 'block';
  }

  document.getElementById('requestBtn').addEventListener('click', function(){
    document.getElementById('choiceOverlay').classList.add('open');
  });
  document.getElementById('forMyselfBtn').addEventListener('click', function(){
    document.getElementById('choiceOverlay').classList.remove('open');
    document.getElementById('formOverlay').classList.add('open');
  });
  document.getElementById('closeFormBtn').addEventListener('click', function(){
    document.getElementById('formOverlay').classList.remove('open');
  });
  document.getElementById('submitFormBtn').addEventListener('click', function(){
    document.getElementById('formOverlay').classList.remove('open');
  });
})();
</script>
</body>
</html>`
