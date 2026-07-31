package web

const pageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>shopservatory</title>
<style>
  :root { color-scheme: light dark; }
  body { font-family: system-ui, sans-serif; margin: 0 auto; max-width: 1100px; padding: 1.5rem; line-height: 1.45; }
  h1 { margin-top: 0; } h2 { margin-top: 2rem; }
  table { border-collapse: collapse; width: 100%; }
  th, td { text-align: left; padding: .4rem .6rem; border-bottom: 1px solid #8884; vertical-align: top; }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 1rem; }
  .card { border: 1px solid #8884; border-radius: 8px; overflow: hidden; }
  .card img { width: 100%; height: 150px; object-fit: cover; background: #8882; display: block; }
  .card .noimg { width: 100%; height: 150px; display: flex; align-items: center; justify-content: center; background: #8882; color: #8889; font-size: .8rem; }
  .card .body { padding: .5rem .7rem; }
  .card .title { font-size: .9rem; display: block; max-height: 3.2em; overflow: hidden; }
  .muted { color: #8889; font-size: .8rem; }
  .approx { color: #8889; font-size: .8rem; }
  .pill { font-size: .7rem; padding: .1rem .4rem; border-radius: 999px; border: 1px solid #8886; }
  fieldset { border: 1px solid #8884; border-radius: 8px; }
  label { display: block; margin: .4rem 0 .1rem; font-size: .85rem; }
  input, select, textarea { width: 100%; padding: .35rem; box-sizing: border-box; }
  .row { display: grid; grid-template-columns: repeat(2, 1fr); gap: .6rem; }
  .logout { position: absolute; top: 1.2rem; right: 1.2rem; margin: 0; }
  .feedbar { display: flex; gap: .5rem; align-items: center; margin: .3rem 0 .9rem; }
  .feedbar input { max-width: 320px; }
  .feedbar button { white-space: nowrap; }
  .sources { display: flex; flex-wrap: wrap; gap: .3rem .8rem; }
  .srcbox { display: inline-flex; align-items: center; gap: .3rem; margin: 0; font-size: .85rem; width: auto; }
  .srcbox input { width: auto; }
  button { padding: .2rem .5rem; cursor: pointer; font-size: .8rem; }
  td.actions { white-space: nowrap; text-align: right; }
  .actions button { margin-left: .25rem; }
  .fold-h { cursor: pointer; user-select: none; }
  .detail { font-size: .8rem; color: #aaa9; }
  .detail code { white-space: pre-wrap; }
  .expander { background: none; border: none; cursor: pointer; font-size: .9rem; padding: 0 .3rem; }
  .cardbtn { margin-top: .4rem; font-size: .75rem; }
  .mthumb { width: 32px; height: 32px; object-fit: cover; vertical-align: middle; margin-right: .45rem; border-radius: 3px; }
  td .title { vertical-align: middle; }
  .status-active { color: #3a3; } .status-sold { color: #c44; } .status-removed { color: #888; }
  .spark { display: flex; align-items: flex-end; gap: 2px; height: 40px; margin: .4rem 0; overflow: hidden; }
  .spark > div { flex: 1 1 0; min-width: 0; max-width: 6px; background: #58a6ff88; }
  .histrow { font-size: .75rem; color: #8889; }
</style>
</head>
<body>
  <form method="post" action="/logout" class="logout"><button type="submit">log out</button></form>
  <h1>🔭 shopservatory</h1>

  <h2 id="form-title">New search</h2>
  <form id="search-form" method="post" action="/searches">
    <fieldset>
      <div>
        <label>Sources <span class="muted" id="f-sources-hint">(select one or more to create a search per source)</span></label>
        <div id="f-sources" class="sources">
          {{range .Sources}}<label class="srcbox"><input type="checkbox" name="source" value="{{.ID}}"> {{.Name}}</label>{{end}}
        </div>
      </div>
      <div>
        <label>Query</label>
        <input name="query" id="f-query" placeholder="keyword, or a browse URL (snkrdunk/suruga-ya)" required>
      </div>
      <div class="row">
        <div><label>Min price</label><input name="min_price" id="f-min" type="number" step="any" placeholder="optional"></div>
        <div><label>Max price</label><input name="max_price" id="f-max" type="number" step="any" placeholder="optional"></div>
      </div>
      <div class="row">
        <div><label>Interval</label><input name="interval" id="f-interval" placeholder="default (e.g. 5m, 1h)"></div>
        <div><label>Params (key=value per line)</label><textarea name="params" id="f-params" rows="2" placeholder="sort=newlyListed"></textarea></div>
      </div>
      <p>
        <button type="submit" id="f-submit">Add search</button>
        <button type="button" id="f-cancel" style="display:none">Cancel edit</button>
      </p>
    </fieldset>
  </form>

  <h2 class="fold-h" id="searches-head">▸ Searches</h2>
  <div id="searches-section" style="display:none">
    <table>
      <thead><tr><th></th><th>#</th><th>Source</th><th>Query</th><th>Interval</th><th>Status</th><th>Last run</th><th></th></tr></thead>
      <tbody id="searches"></tbody>
    </table>
    <p class="muted" id="searches-empty" style="display:none">No searches yet — add one above.</p>
  </div>

  <h2 class="fold-h" id="monitors-head">▸ Monitoring</h2>
  <div id="monitors-section" style="display:none">
    <form id="monitor-form">
      <div class="feedbar">
        <input id="m-url" placeholder="paste an item URL to track its price…" autocomplete="off">
        <input id="m-interval" placeholder="interval — default (e.g. 1h, 6h)" style="max-width:180px" autocomplete="off">
        <button type="submit">Monitor URL</button>
      </div>
    </form>
    <table>
      <thead><tr><th></th><th>Source</th><th>Item</th><th>Price</th><th>Status</th><th>Interval</th><th>Last checked</th><th></th></tr></thead>
      <tbody id="monitors"></tbody>
    </table>
    <p class="muted" id="monitors-empty" style="display:none">Nothing monitored yet — paste an item URL above, or click “monitor” on a find.</p>
  </div>

  <h2 class="fold-h" id="settings-head">▸ Settings</h2>
  <div id="settings-section" style="display:none">
    <form id="settings-form">
      <div class="row">
        <div><label>Display currency</label><input id="s-currency" placeholder="e.g. EUR, USD, JPY"></div>
        <div><label>Default search interval</label><input id="s-search" placeholder="e.g. 5m, 1h"></div>
        <div><label>Default monitor interval</label><input id="s-monitor" placeholder="e.g. 1h, 6h"></div>
        <div><label>Telegram chat ID</label><input id="s-telegram" placeholder="optional"></div>
      </div>
      <div class="row">
        <button type="submit">Save settings</button>
        <span class="muted" id="settings-status" style="font-size:.8rem"></span>
      </div>
    </form>
    <h3>Change password</h3>
    <form id="password-form">
      <div class="row">
        <div><label>Current password</label><input id="p-current" type="password" autocomplete="current-password"></div>
        <div><label>New password</label><input id="p-new" type="password" autocomplete="new-password" placeholder="at least 8 characters"></div>
      </div>
      <div class="row">
        <button type="submit">Change password</button>
        <span class="muted" id="password-status" style="font-size:.8rem"></span>
      </div>
    </form>
  </div>

  <h2 class="fold-h" id="users-head" style="display:none">▸ Users</h2>
  <div id="users-section" style="display:none">
    <form id="user-form">
      <div class="row">
        <div><label>Name</label><input id="u-name" placeholder="optional"></div>
        <div><label>Email</label><input id="u-email" required></div>
        <div><label>Password</label><input id="u-password" type="password" autocomplete="new-password" placeholder="optional (for password login)"></div>
        <div><label><input type="checkbox" id="u-admin"> Admin</label></div>
      </div>
      <div class="row">
        <button type="submit" id="user-submit">Add user</button>
        <button type="button" id="user-cancel" style="display:none">Cancel edit</button>
        <span class="muted" id="user-status" style="font-size:.8rem"></span>
      </div>
    </form>
    <table>
      <thead><tr><th>#</th><th>Name</th><th>Email</th><th>Login</th><th>Role</th><th></th></tr></thead>
      <tbody id="users"></tbody>
    </table>
  </div>

  <h2><span id="feed-label">Finds</span> <span class="muted" id="feed-status" style="font-size:.8rem;font-weight:normal"></span></h2>
  <div class="feedbar">
    <input id="feed-filter" placeholder="filter results…" autocomplete="off">
    <button type="button" id="feed-prev">‹ prev</button>
    <span class="muted" id="feed-pageinfo"></span>
    <button type="button" id="feed-next">next ›</button>
  </div>
  <p class="muted" id="feed-empty" style="display:none">Nothing found yet. Once searches run, new items appear here.</p>
  <div class="grid" id="feed"></div>
  <div class="feedbar" id="feed-pager-bottom" style="justify-content:center;margin-top:.9rem">
    <button type="button" id="feed-prev2">‹ prev</button>
    <span class="muted" id="feed-pageinfo2"></span>
    <button type="button" id="feed-next2">next ›</button>
  </div>

  <p class="muted" style="margin-top:2rem">shopservatory · live feed</p>

  <script>
  (function () {
    var INTERVAL = 15000;
    var expanded = {};
    var sources = [{{range .Sources}}{id:"{{.ID}}",name:"{{.Name}}"},{{end}}];
    function sourceName(id){ for (var i=0;i<sources.length;i++) if (sources[i].id===id) return sources[i].name; return id; }

    function el(tag, cls, text) { var e=document.createElement(tag); if(cls)e.className=cls; if(text!=null)e.textContent=text; return e; }

    function timeLeft(ends) {
      if (!ends) return '';
      var d = new Date(ends);
      if (isNaN(d)) return '';
      var mins = Math.floor((d - Date.now()) / 60000);
      if (mins <= 0) return 'ended';
      var days = Math.floor(mins / 1440), hours = Math.floor((mins % 1440) / 60);
      if (days > 0) return days + 'd ' + hours + 'h left';
      if (hours > 0) return hours + 'h ' + (mins % 60) + 'm left';
      return mins + 'm left';
    }

    function timeLeftSec(ends) {
      if (!ends) return '';
      var d = new Date(ends);
      if (isNaN(d)) return '';
      var secs = Math.floor((d - Date.now()) / 1000);
      if (secs <= 0) return 'ended';
      var days = Math.floor(secs / 86400), h = Math.floor(secs % 86400 / 3600), m = Math.floor(secs % 3600 / 60), s = secs % 60;
      if (days > 0) return days + 'd ' + h + 'h ' + m + 'm ' + s + 's left';
      if (h > 0) return h + 'h ' + m + 'm ' + s + 's left';
      if (m > 0) return m + 'm ' + s + 's left';
      return s + 's left';
    }

    function endDateLabel(ends) {
      var d = new Date(ends);
      if (isNaN(d)) return '';
      function p(n){ return (n < 10 ? '0' : '') + n; }
      return 'ends ' + p(d.getDate()) + '.' + p(d.getMonth()+1) + '.' + d.getFullYear() + ' ' + p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
    }

    function sparkPoints(h, n) {
      if (h.length <= n) return h;
      var out = [];
      for (var i = 0; i < n; i++) {
        var start = Math.floor(i*h.length/n), end = Math.max(Math.floor((i+1)*h.length/n), start+1);
        var p = h[end-1];
        out.push({price: p.price, status: p.status, at: (end-start > 1 ? h[start].at+' … ' : '') + p.at});
      }
      return out;
    }

    function tickCountdowns() {
      var els = document.querySelectorAll('[data-ends]');
      for (var i = 0; i < els.length; i++) els[i].textContent = timeLeftSec(els[i].getAttribute('data-ends'));
    }

    function action(url) { return fetch(url, {method:'POST'}).then(refresh).catch(function(){}); }

    function btn(label, fn) { var b=el('button',null,label); b.type='button'; b.onclick=fn; return b; }

    function renderSearches(list) {
      var tb = document.getElementById('searches');
      tb.replaceChildren();
      document.getElementById('searches-empty').style.display = list.length ? 'none' : '';
      list.forEach(function (se) {
        var tr = el('tr');
        var exp = el('td');
        var t = el('button','expander', expanded[se.id] ? '▾' : '▸'); t.type='button';
        t.onclick = function(){ expanded[se.id] = !expanded[se.id]; renderSearches(list); };
        exp.appendChild(t);
        tr.appendChild(exp);
        tr.appendChild(el('td', null, String(se.id)));
        tr.appendChild(el('td', null, sourceName(se.source)));
        var qcell = el('td', 'query', se.query.length > 70 ? se.query.slice(0, 70) + '…' : se.query);
        qcell.title = se.query;
        tr.appendChild(qcell);
        tr.appendChild(el('td', null, se.interval));
        var st = el('td'); st.appendChild(el('span','pill', se.enabled ? 'enabled' : 'paused')); tr.appendChild(st);
        tr.appendChild(el('td', 'muted', se.lastRun));

        var act = el('td','actions');
        act.appendChild(btn('run', function(){ action('/searches/'+se.id+'/run'); }));
        act.appendChild(btn(se.enabled ? 'pause' : 'resume', function(){ action('/searches/'+se.id+'/toggle'); }));
        act.appendChild(btn('edit', function(){ startEdit(se); }));
        act.appendChild(btn('delete', function(){ if(confirm('Delete this search and its history?')) action('/searches/'+se.id+'/delete'); }));
        tr.appendChild(act);
        tb.appendChild(tr);

        if (expanded[se.id]) {
          var dr = el('tr'); var dc = el('td','detail'); dc.colSpan = 8;
          var bits = [];
          if (se.minPrice) bits.push('min: ' + se.minPrice);
          if (se.maxPrice) bits.push('max: ' + se.maxPrice);
          var pk = se.params ? Object.keys(se.params) : [];
          if (pk.length) bits.push('params:');
          if (!bits.length) bits.push('no extra filters');
          dc.appendChild(document.createTextNode(bits.join('  ·  ')));
          if (pk.length) {
            var pre = el('code'); pre.textContent = '\n' + pk.map(function(k){return '  '+k+'='+se.params[k];}).join('\n');
            dc.appendChild(pre);
          }
          dr.appendChild(dc); tb.appendChild(dr);
        }
      });
    }

    function sourceBoxes(){ return document.querySelectorAll('#f-sources input[name=source]'); }
    function setSources(ids){ sourceBoxes().forEach(function(b){ b.checked = ids.indexOf(b.value) >= 0; }); }

    function startEdit(se) {
      document.getElementById('form-title').textContent = 'Edit search #' + se.id;
      setSources([se.source]);
      document.getElementById('f-sources-hint').textContent = '(editing one search)';
      document.getElementById('f-query').value = se.query;
      document.getElementById('f-min').value = se.minPrice || '';
      document.getElementById('f-max').value = se.maxPrice || '';
      document.getElementById('f-interval').value = se.interval;
      var pk = se.params ? Object.keys(se.params) : [];
      document.getElementById('f-params').value = pk.map(function(k){return k+'='+se.params[k];}).join('\n');
      document.getElementById('search-form').action = '/searches/' + se.id + '/update';
      document.getElementById('f-submit').textContent = 'Update search';
      document.getElementById('f-cancel').style.display = '';
      window.scrollTo({top:0, behavior:'smooth'});
    }
    function resetForm() {
      document.getElementById('form-title').textContent = 'New search';
      var f = document.getElementById('search-form'); f.reset(); f.action = '/searches';
      document.getElementById('f-sources-hint').textContent = '(select one or more to create a search per source)';
      document.getElementById('f-submit').textContent = 'Add search';
      document.getElementById('f-cancel').style.display = 'none';
    }
    document.getElementById('search-form').addEventListener('submit', function(e){
      var any = false; sourceBoxes().forEach(function(b){ if (b.checked) any = true; });
      if (!any) { e.preventDefault(); alert('Select at least one source.'); }
    });
    document.getElementById('f-cancel').onclick = resetForm;

    (function(){
      var head = document.getElementById('searches-head');
      head.onclick = function(){
        var sec = document.getElementById('searches-section');
        var open = sec.style.display !== 'none';
        sec.style.display = open ? 'none' : '';
        head.textContent = (open ? '▸' : '▾') + ' Searches';
      };
    })();

    (function(){
      var head = document.getElementById('monitors-head');
      head.onclick = function(){
        var sec = document.getElementById('monitors-section');
        var open = sec.style.display !== 'none';
        sec.style.display = open ? 'none' : '';
        head.textContent = (open ? '▸' : '▾') + ' Monitoring';
      };
    })();

    (function(){
      var head = document.getElementById('settings-head');
      head.onclick = function(){
        var sec = document.getElementById('settings-section');
        var open = sec.style.display !== 'none';
        sec.style.display = open ? 'none' : '';
        head.textContent = (open ? '▸' : '▾') + ' Settings';
      };
    })();

    var settingsDirty = false;
    ['s-currency','s-search','s-monitor','s-telegram'].forEach(function(id){
      document.getElementById(id).addEventListener('input', function(){ settingsDirty = true; });
    });
    function renderSettings(st){
      if (settingsDirty) return;
      document.getElementById('s-currency').value = st.currency || '';
      document.getElementById('s-search').value = st.searchInterval || '';
      document.getElementById('s-monitor').value = st.monitorInterval || '';
      document.getElementById('s-telegram').value = st.telegramChatId || '';
    }
    document.getElementById('settings-form').addEventListener('submit', function(e){
      e.preventDefault();
      var f = new URLSearchParams();
      f.set('currency', document.getElementById('s-currency').value.trim());
      f.set('search_interval', document.getElementById('s-search').value.trim());
      f.set('monitor_interval', document.getElementById('s-monitor').value.trim());
      f.set('telegram_chat_id', document.getElementById('s-telegram').value.trim());
      var status = document.getElementById('settings-status');
      status.textContent = 'saving…';
      fetch('/settings', {method:'POST', headers:{'Content-Type':'application/x-www-form-urlencoded'}, body:f.toString()})
        .then(function(r){
          if (r.status===204 || r.ok) { settingsDirty = false; status.textContent = 'saved'; refresh(); }
          else { status.textContent = 'could not save'; }
        })
        .catch(function(){ status.textContent = 'could not save'; });
    });

    function postForm(url, params, statusEl, okText, onok){
      if (statusEl) statusEl.textContent = 'saving…';
      return fetch(url, {method:'POST', headers:{'Content-Type':'application/x-www-form-urlencoded'}, body:params.toString()})
        .then(function(r){
          if (r.status===204 || r.ok){ if(statusEl) statusEl.textContent = okText || 'saved'; if(onok) onok(); refresh(); }
          else { return r.text().then(function(t){ if(statusEl) statusEl.textContent = t || 'failed'; }); }
        }).catch(function(){ if(statusEl) statusEl.textContent = 'failed'; });
    }

    document.getElementById('password-form').addEventListener('submit', function(e){
      e.preventDefault();
      var f = new URLSearchParams();
      f.set('current_password', document.getElementById('p-current').value);
      f.set('new_password', document.getElementById('p-new').value);
      postForm('/password', f, document.getElementById('password-status'), 'password changed', function(){
        document.getElementById('p-current').value=''; document.getElementById('p-new').value='';
      });
    });

    (function(){
      var head = document.getElementById('users-head');
      head.onclick = function(){
        var sec = document.getElementById('users-section');
        var open = sec.style.display !== 'none';
        sec.style.display = open ? 'none' : '';
        head.textContent = (open ? '▸' : '▾') + ' Users';
      };
    })();

    var meId = 0, editUserID = 0;
    function renderMe(me){
      meId = me.id || 0;
      var head = document.getElementById('users-head');
      if (me.isAdmin) { head.style.display = ''; }
      else { head.style.display = 'none'; document.getElementById('users-section').style.display = 'none'; }
    }
    function resetUserForm(){
      editUserID = 0;
      document.getElementById('user-form').reset();
      document.getElementById('user-submit').textContent = 'Add user';
      document.getElementById('user-cancel').style.display = 'none';
      document.getElementById('u-password').placeholder = 'optional (for password login)';
      document.getElementById('user-status').textContent = '';
    }
    function startUserEdit(u){
      editUserID = u.id;
      document.getElementById('u-name').value = u.name || '';
      document.getElementById('u-email').value = u.email || '';
      document.getElementById('u-password').value = '';
      document.getElementById('u-password').placeholder = 'leave blank to keep current';
      document.getElementById('u-admin').checked = !!u.isAdmin;
      document.getElementById('user-submit').textContent = 'Update user #' + u.id;
      document.getElementById('user-cancel').style.display = '';
      window.scrollTo({top: document.getElementById('users-head').offsetTop, behavior:'smooth'});
    }
    document.getElementById('user-cancel').onclick = resetUserForm;
    function renderUsers(list){
      var tb = document.getElementById('users'); tb.replaceChildren();
      list.forEach(function(u){
        var tr = el('tr');
        tr.appendChild(el('td', null, String(u.id)));
        tr.appendChild(el('td', null, u.name));
        tr.appendChild(el('td', null, u.email));
        var login = []; if (u.hasPassword) login.push('password'); if (u.oidc) login.push('SSO');
        tr.appendChild(el('td', 'muted', login.length ? login.join(' + ') : '—'));
        var role = el('td'); role.appendChild(el('span','pill', u.isAdmin ? 'admin' : 'user')); tr.appendChild(role);
        var act = el('td','actions');
        act.appendChild(btn('edit', function(){ startUserEdit(u); }));
        if (u.id !== meId) {
          act.appendChild(btn('delete', function(){ if (confirm('Delete user ' + u.email + ' and all their data?')) action('/admin/users/' + u.id + '/delete'); }));
        }
        tr.appendChild(act); tb.appendChild(tr);
      });
    }
    document.getElementById('user-form').addEventListener('submit', function(e){
      e.preventDefault();
      var email = document.getElementById('u-email').value.trim();
      if (!email) { document.getElementById('user-status').textContent = 'email is required'; return; }
      var f = new URLSearchParams();
      f.set('name', document.getElementById('u-name').value.trim());
      f.set('email', email);
      var pw = document.getElementById('u-password').value; if (pw) f.set('password', pw);
      if (document.getElementById('u-admin').checked) f.set('admin', 'on');
      var url = editUserID ? '/admin/users/' + editUserID + '/update' : '/admin/users';
      postForm(url, f, document.getElementById('user-status'), editUserID ? 'updated' : 'added', resetUserForm);
    });

    document.getElementById('monitor-form').addEventListener('submit', function(e){
      e.preventDefault();
      var inp = document.getElementById('m-url'); var u = inp.value.trim();
      if (!u) return;
      var f = new URLSearchParams(); f.set('url', u);
      var iv = document.getElementById('m-interval').value.trim(); if (iv) f.set('interval', iv);
      fetch('/monitors', {method:'POST', headers:{'Content-Type':'application/x-www-form-urlencoded'}, body:f.toString()})
        .then(function(r){
          if (r.status===204 || r.ok) { inp.value=''; refresh(); }
          else { r.text().then(function(t){ alert(t || 'Could not monitor that URL'); }); }
        }).catch(function(){});
    });

    var monExpanded = {};
    function renderMonitors(list) {
      var tb = document.getElementById('monitors');
      tb.replaceChildren();
      document.getElementById('monitors-empty').style.display = list.length ? 'none' : '';
      list.forEach(function (m) {
        var tr = el('tr');
        var exp = el('td');
        var t = el('button','expander', monExpanded[m.id] ? '▾' : '▸'); t.type='button';
        t.onclick = function(){ monExpanded[m.id] = !monExpanded[m.id]; renderMonitors(list); };
        exp.appendChild(t); tr.appendChild(exp);
        tr.appendChild(el('td', null, sourceName(m.source)));
        var itd = el('td');
        if (m.imageUrl) { var im = el('img','mthumb'); im.src='/img?u='+encodeURIComponent(m.imageUrl); im.loading='lazy'; im.onerror=function(){ im.style.display='none'; }; itd.appendChild(im); }
        var a = el('a','title'); a.href=m.url; a.target='_blank'; a.rel='noopener'; a.textContent = m.title || m.url; itd.appendChild(a); tr.appendChild(itd);
        var ptd = el('td'); ptd.textContent = m.price || ''; if (m.priceApprox) { ptd.appendChild(el('span','approx','  '+m.priceApprox)); } tr.appendChild(ptd);
        var std = el('td'); std.appendChild(el('span','status-'+(m.status||'active'), m.status||'active'));
        var mtl = timeLeftSec(m.ends);
        if (mtl) {
          var mts=el('div','muted',mtl); mts.style.fontSize='.75rem'; mts.setAttribute('data-ends', m.ends);
          mts.title = endDateLabel(m.ends);
          std.appendChild(mts);
        }
        tr.appendChild(std);
        tr.appendChild(el('td','muted', m.interval || ''));
        tr.appendChild(el('td','muted', m.lastChecked));
        var act = el('td','actions');
        act.appendChild(btn('check', function(){ action('/monitors/'+m.id+'/run'); }));
        act.appendChild(btn('edit', function(){
          var v = prompt('Refresh interval (e.g. 30m, 1h, 6h):', m.interval || '1h');
          if (!v) return;
          var f = new URLSearchParams(); f.set('interval', v.trim());
          fetch('/monitors/'+m.id+'/update', {method:'POST', headers:{'Content-Type':'application/x-www-form-urlencoded'}, body:f.toString()})
            .then(function(r){ if (r.status===204 || r.ok) { refresh(); } else { alert('Invalid interval — use e.g. 30m, 1h, 6h'); } });
        }));
        act.appendChild(btn('delete', function(){ if(confirm('Stop monitoring this item?')) action('/monitors/'+m.id+'/delete'); }));
        tr.appendChild(act); tb.appendChild(tr);

        if (monExpanded[m.id]) {
          var dr = el('tr'); var dc = el('td','detail'); dc.colSpan = 8;
          var h = m.history || [];
          if (h.length) {
            var shown = sparkPoints(h, 120);
            var max = 0; shown.forEach(function(p){ if (p.price > max) max = p.price; });
            var sp = el('div','spark');
            shown.forEach(function(p){ var bar = el('div'); bar.style.height = (max>0 ? Math.max(2, Math.round(p.price/max*40)) : 2)+'px'; bar.title = p.at+': '+p.price+(p.status&&p.status!=='active'?' ('+p.status+')':''); sp.appendChild(bar); });
            dc.appendChild(sp);
            var first = h[0], last = h[h.length-1];
            dc.appendChild(el('div','histrow', h.length+' checks · first '+first.price+' ('+first.at+') · latest '+last.price+' ('+last.at+')'));
          } else {
            dc.appendChild(el('div','histrow','no price history yet — it will fill in as checks run'));
          }
          dr.appendChild(dc); tb.appendChild(dr);
        }
      });
    }

    function card(item) {
      var c = el('div', 'card');
      var a = el('a'); a.href=item.url; a.target='_blank'; a.rel='noopener';
      if (item.imageUrl) {
        var img = el('img'); img.src='/img?u='+encodeURIComponent(item.imageUrl); img.loading='lazy'; img.alt='';
        img.onerror = function(){ if (img.parentNode) img.parentNode.replaceChild(el('div','noimg','no image'), img); };
        a.appendChild(img);
      } else {
        a.appendChild(el('div','noimg','no image'));
      }
      c.appendChild(a);
      var body = el('div','body');
      var title = el('a','title'); title.href=item.url; title.target='_blank'; title.rel='noopener'; title.textContent=item.title;
      body.appendChild(title);
      if (item.saleType === 'auction') {
        var ap=el('span','pill','auction'); ap.style.marginRight='.3rem'; body.appendChild(ap);
        var tl = timeLeft(item.ends);
        if (tl) { var ts=el('span','muted',tl); ts.style.fontSize='.75rem'; ts.title = endDateLabel(item.ends); body.appendChild(ts); }
      }
      var price = el('div','muted'); price.textContent = item.price || '';
      if (item.priceApprox) { var ap=el('span','approx','  '+item.priceApprox); price.appendChild(ap); }
      body.appendChild(price);
      var label = sourceName(item.source) + (item.searchId ? ' #' + item.searchId : '');
      body.appendChild(el('div','muted', label + ' · ' + item.seen));
      var mon = el('button','cardbtn','monitor'); mon.type='button';
      mon.onclick = function(){ monitorItem(item, mon); };
      body.appendChild(mon);
      c.appendChild(body);
      return c;
    }

    function monitorItem(item, btn) {
      var f = new URLSearchParams();
      f.set('source', item.source||''); f.set('external_id', item.externalId||'');
      f.set('url', item.url||''); f.set('title', item.title||''); f.set('image_url', item.imageUrl||'');
      f.set('currency', item.currency||''); f.set('sale_type', item.saleType||''); f.set('ends', item.ends||'');
      f.set('price', item.priceValue!=null ? String(item.priceValue) : '');
      if (btn) { btn.disabled = true; btn.textContent = '…'; }
      fetch('/monitors', {method:'POST', headers:{'Content-Type':'application/x-www-form-urlencoded'}, body:f.toString()})
        .then(function(r){
          if (r.status===204 || r.ok) { if(btn){ btn.textContent='monitoring'; } refresh(); }
          else { if(btn){ btn.disabled=false; btn.textContent='monitor'; } alert('Could not add to monitoring'); }
        }).catch(function(){ if(btn){ btn.disabled=false; btn.textContent='monitor'; } });
    }
    var feedFilter = '';
    var feedPage = 1;

    function renderFeed(list, total, page, pages) {
      feedPage = page;
      var feed = document.getElementById('feed');
      feed.replaceChildren.apply(feed, list.map(card));
      document.getElementById('feed-empty').style.display = total ? 'none' : '';
      document.getElementById('feed-status').textContent =
        '· ' + total + (feedFilter ? ' matching' : '') + ' · updated ' + new Date().toLocaleTimeString();
      var info = 'page ' + page + ' / ' + pages;
      ['','2'].forEach(function(sfx){
        document.getElementById('feed-pageinfo'+sfx).textContent = info;
        document.getElementById('feed-prev'+sfx).disabled = page <= 1;
        document.getElementById('feed-next'+sfx).disabled = page >= pages;
      });
      document.getElementById('feed-pager-bottom').style.display = pages > 1 ? '' : 'none';
    }

    function refresh() {
      var params = new URLSearchParams();
      params.set('page', feedPage);
      if (feedFilter) params.set('q', feedFilter);
      return fetch('/api/state?' + params.toString(), {headers:{'Accept':'application/json'}})
        .then(function(r){ return r.ok ? r.json() : Promise.reject(r.status); })
        .then(function(s){
          renderSearches(s.searches||[]); renderMonitors(s.monitors||[]);
          renderFeed(s.listings||[], s.listingsTotal||0, s.listingsPage||1, s.listingsPages||1);
          renderSettings(s.settings||{}); renderMe(s.me||{}); renderUsers(s.users||[]);
        })
        .catch(function(){});
    }

    var filterTimer;
    document.getElementById('feed-filter').addEventListener('input', function(e){
      feedFilter = e.target.value.trim(); feedPage = 1;
      clearTimeout(filterTimer); filterTimer = setTimeout(refresh, 300);
    });
    function turnPage(delta) {
      feedPage = Math.max(1, feedPage + delta);
      refresh().then(function(){ window.scrollTo({top: document.getElementById('feed-label').offsetTop - 20, behavior: 'instant'}); });
    }
    ['','2'].forEach(function(sfx){
      document.getElementById('feed-prev'+sfx).onclick = function(){ turnPage(-1); };
      document.getElementById('feed-next'+sfx).onclick = function(){ turnPage(1); };
    });

    refresh();
    setInterval(refresh, INTERVAL);
    setInterval(tickCountdowns, 1000);
  })();
  </script>
</body>
</html>`
