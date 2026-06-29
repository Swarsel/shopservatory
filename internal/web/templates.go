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
  .spark { display: flex; align-items: flex-end; gap: 2px; height: 40px; margin: .4rem 0; }
  .spark > div { width: 6px; background: #58a6ff88; }
  .histrow { font-size: .75rem; color: #8889; }
</style>
</head>
<body>
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
        <div><label>Interval</label><input name="interval" id="f-interval" value="5m" placeholder="e.g. 5m, 1h"></div>
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
        <input id="m-interval" placeholder="interval (e.g. 1h)" style="max-width:140px" autocomplete="off">
        <button type="submit">Monitor URL</button>
      </div>
    </form>
    <table>
      <thead><tr><th></th><th>Source</th><th>Item</th><th>Price</th><th>Status</th><th>Every</th><th>Checked</th><th></th></tr></thead>
      <tbody id="monitors"></tbody>
    </table>
    <p class="muted" id="monitors-empty" style="display:none">Nothing monitored yet — paste an item URL above, or click “📌 monitor” on a find.</p>
  </div>

  <h2><span id="feed-label">Recent finds</span> <span class="muted" id="feed-status" style="font-size:.8rem;font-weight:normal"></span></h2>
  <div class="feedbar">
    <input id="feed-filter" placeholder="filter results…" autocomplete="off">
    <button type="button" id="feed-toggle">Show all</button>
  </div>
  <p class="muted" id="feed-empty" style="display:none">Nothing found yet. Once searches run, new items appear here.</p>
  <div class="grid" id="feed"></div>

  <p class="muted" style="margin-top:2rem">shopservatory · live feed</p>

  <script>
  (function () {
    var INTERVAL = 15000;
    var expanded = {};
    var sources = [{{range .Sources}}{id:"{{.ID}}",name:"{{.Name}}"},{{end}}];
    function sourceName(id){ for (var i=0;i<sources.length;i++) if (sources[i].id===id) return sources[i].name; return id; }

    function el(tag, cls, text) { var e=document.createElement(tag); if(cls)e.className=cls; if(text!=null)e.textContent=text; return e; }

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
        tr.appendChild(el('td', null, se.source));
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
      document.getElementById('f-interval').value = '5m';
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
        var std = el('td'); std.appendChild(el('span','status-'+(m.status||'active'), m.status||'active')); tr.appendChild(std);
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
            var max = 0; h.forEach(function(p){ if (p.price > max) max = p.price; });
            var sp = el('div','spark');
            h.forEach(function(p){ var bar = el('div'); bar.style.height = (max>0 ? Math.max(2, Math.round(p.price/max*40)) : 2)+'px'; bar.title = p.at+': '+p.price+(p.status&&p.status!=='active'?' ('+p.status+')':''); sp.appendChild(bar); });
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
      if (item.saleType === 'auction') { var ap=el('span','pill','auction'); ap.style.marginRight='.3rem'; body.appendChild(ap); }
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
      f.set('currency', item.currency||''); f.set('sale_type', item.saleType||'');
      f.set('price', item.priceValue!=null ? String(item.priceValue) : '');
      if (btn) { btn.disabled = true; btn.textContent = '…'; }
      fetch('/monitors', {method:'POST', headers:{'Content-Type':'application/x-www-form-urlencoded'}, body:f.toString()})
        .then(function(r){
          if (r.status===204 || r.ok) { if(btn){ btn.textContent='monitoring'; } refresh(); }
          else { if(btn){ btn.disabled=false; btn.textContent='monitor'; } alert('Could not add to monitoring'); }
        }).catch(function(){ if(btn){ btn.disabled=false; btn.textContent='monitor'; } });
    }
    var rawListings = [];
    var feedFilter = '';
    var showAll = false;

    function paintFeed() {
      var f = feedFilter.toLowerCase();
      var shown = f ? rawListings.filter(function(it){
        return (it.title||'').toLowerCase().indexOf(f) >= 0 || sourceName(it.source).toLowerCase().indexOf(f) >= 0;
      }) : rawListings;
      var feed = document.getElementById('feed');
      feed.replaceChildren.apply(feed, shown.map(card));
      document.getElementById('feed-empty').style.display = rawListings.length ? 'none' : '';
      document.getElementById('feed-status').textContent =
        '· ' + (f ? shown.length + ' of ' + rawListings.length : rawListings.length) +
        ' · updated ' + new Date().toLocaleTimeString();
    }

    function renderFeed(list) { rawListings = list; paintFeed(); }

    function refresh() {
      return fetch('/api/state' + (showAll ? '?all=1' : ''), {headers:{'Accept':'application/json'}})
        .then(function(r){ return r.ok ? r.json() : Promise.reject(r.status); })
        .then(function(s){ renderSearches(s.searches||[]); renderMonitors(s.monitors||[]); renderFeed(s.listings||[]); })
        .catch(function(){});
    }

    document.getElementById('feed-filter').addEventListener('input', function(e){ feedFilter = e.target.value.trim(); paintFeed(); });
    document.getElementById('feed-toggle').onclick = function(){
      showAll = !showAll;
      this.textContent = showAll ? 'Show recent' : 'Show all';
      document.getElementById('feed-label').textContent = showAll ? 'All finds' : 'Recent finds';
      refresh();
    };

    refresh();
    setInterval(refresh, INTERVAL);
  })();
  </script>
</body>
</html>`
