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
  button { padding: .2rem .5rem; cursor: pointer; font-size: .8rem; }
  td.actions { white-space: nowrap; text-align: right; }
  .actions button { margin-left: .25rem; }
  .fold-h { cursor: pointer; user-select: none; }
  .detail { font-size: .8rem; color: #aaa9; }
  .detail code { white-space: pre-wrap; }
  .expander { background: none; border: none; cursor: pointer; font-size: .9rem; padding: 0 .3rem; }
</style>
</head>
<body>
  <h1>🔭 shopservatory</h1>

  <h2 id="form-title">New search</h2>
  <form id="search-form" method="post" action="/searches">
    <fieldset>
      <div class="row">
        <div>
          <label>Source</label>
          <select name="source" id="f-source" required>
            {{range .Sources}}<option value="{{.ID}}">{{.Name}}</option>{{end}}
          </select>
        </div>
        <div>
          <label>Query</label>
          <input name="query" id="f-query" placeholder="keyword, or a browse URL (snkrdunk/suruga-ya)" required>
        </div>
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

  <h2>Recent finds <span class="muted" id="feed-status" style="font-size:.8rem;font-weight:normal"></span></h2>
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
        tr.appendChild(el('td', null, se.query));
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

    function startEdit(se) {
      document.getElementById('form-title').textContent = 'Edit search #' + se.id;
      document.getElementById('f-source').value = se.source;
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
      document.getElementById('f-submit').textContent = 'Add search';
      document.getElementById('f-cancel').style.display = 'none';
    }
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
      var price = el('div','muted'); price.textContent = item.price || '';
      if (item.priceApprox) { var ap=el('span','approx','  '+item.priceApprox); price.appendChild(ap); }
      body.appendChild(price);
      var label = sourceName(item.source) + (item.searchId ? ' #' + item.searchId : '');
      body.appendChild(el('div','muted', label + ' · ' + item.seen));
      c.appendChild(body);
      return c;
    }
    function renderFeed(list) {
      var feed = document.getElementById('feed');
      feed.replaceChildren.apply(feed, list.map(card));
      document.getElementById('feed-empty').style.display = list.length ? 'none' : '';
      document.getElementById('feed-status').textContent = '· updated ' + new Date().toLocaleTimeString();
    }

    function refresh() {
      return fetch('/api/state', {headers:{'Accept':'application/json'}})
        .then(function(r){ return r.ok ? r.json() : Promise.reject(r.status); })
        .then(function(s){ renderSearches(s.searches||[]); renderFeed(s.listings||[]); })
        .catch(function(){});
    }

    refresh();
    setInterval(refresh, INTERVAL);
  })();
  </script>
</body>
</html>`
