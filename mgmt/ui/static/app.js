// twd admin UI: progressive enhancement over server-rendered pages.
//  - every 3 s, re-fetch the current page and swap the [data-live] regions
//    (the server's templates stay the single renderer), and fetch
//    /v1/status for the header counters;
//  - forms with data-api perform their mutation with fetch against /v1
//    (DELETE + query from the form fields) after a confirm dialog, sending
//    X-Twd-UI: 1 (a custom header: cross-origin pages would need a CORS
//    preflight mgmt never answers). Without JavaScript the same forms POST
//    to their /ui/... action.
// Nothing is stored client-side: no cookies, no localStorage, no tokens
// (the connection's tailnet identity is the credential).
(function () {
  'use strict';
  var REFRESH_MS = 3000;
  var $ = function (sel, el) { return (el || document).querySelector(sel); };
  var $$ = function (sel, el) { return Array.prototype.slice.call((el || document).querySelectorAll(sel)); };

  function toast(kind, head, text, link) {
    var box = $('#toasts');
    if (!box) return;
    var t = document.createElement('div');
    t.className = 'toast ' + kind;
    var c = document.createElement('span');
    c.className = 'code';
    c.textContent = head;
    t.appendChild(c);
    if (text) t.appendChild(document.createTextNode(' ' + text));
    if (link) {
      var a = document.createElement('a');
      a.href = link; a.rel = 'noreferrer'; a.target = '_blank'; a.textContent = link;
      t.appendChild(a);
    }
    box.appendChild(t);
    setTimeout(function () { if (t.parentNode) t.parentNode.removeChild(t); }, kind === 'bad' ? 12000 : 5000);
  }

  // ---- live refresh ----
  var state = $('#live-state');
  var busy = false;
  function setState(s) { if (state) state.textContent = s; }

  function refreshPage() {
    return fetch(location.href, { headers: { 'Accept': 'text/html' }, cache: 'no-store', credentials: 'same-origin' })
      .then(function (res) {
        if (!res.ok) throw new Error('page ' + res.status);
        return res.text();
      })
      .then(function (html) {
        var doc = new DOMParser().parseFromString(html, 'text/html');
        $$('[data-live]').forEach(function (el) {
          var name = el.getAttribute('data-live');
          var fresh = doc.querySelector('[data-live="' + name + '"]');
          if (fresh && fresh.innerHTML !== el.innerHTML) el.innerHTML = fresh.innerHTML;
        });
      });
  }

  function refreshStatus() {
    return fetch('/v1/status', { headers: { 'Accept': 'application/json' }, cache: 'no-store', credentials: 'same-origin' })
      .then(function (res) { return res.ok ? res.json() : null; })
      .then(function (st) {
        if (!st) return;
        var val = function (path) {
          return path.split('.').reduce(function (o, k) { return o == null ? o : o[k]; }, st);
        };
        $$('[data-stat]').forEach(function (el) {
          var v = val(el.getAttribute('data-stat'));
          if (v !== undefined && v !== null) el.textContent = String(v);
        });
      });
  }

  function tick() {
    if (document.hidden || busy) return;
    if (document.activeElement && /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName)) return;
    busy = true;
    Promise.all([refreshPage(), refreshStatus()])
      .then(function () { setState('live · refreshed ' + new Date().toISOString().slice(11, 19) + 'Z'); })
      .catch(function (e) { setState('refresh failed: ' + e.message); })
      .then(function () { busy = false; });
  }
  setState('live refresh every ' + (REFRESH_MS / 1000) + ' s');
  setInterval(tick, REFRESH_MS);

  // ---- mutations ----
  function queryOf(form) {
    var q = [];
    $$('input[name]', form).forEach(function (inp) {
      if (inp.type === 'checkbox') {
        if (inp.checked) q.push(encodeURIComponent(inp.name) + '=true');
      } else if (inp.value.trim() !== '') {
        q.push(encodeURIComponent(inp.name) + '=' + encodeURIComponent(inp.value.trim()));
      }
    });
    return q.length ? '?' + q.join('&') : '';
  }

  document.addEventListener('submit', function (ev) {
    var form = ev.target;
    if (!form || !form.getAttribute || !form.getAttribute('data-api')) return;
    ev.preventDefault();
    var msg = form.getAttribute('data-confirm') || 'Proceed?';
    if (!window.confirm(msg)) return;
    var url = form.getAttribute('data-api') + queryOf(form);
    var method = form.getAttribute('data-method') || 'DELETE';
    var after = form.getAttribute('data-after') || location.href;
    var btn = $('button[type=submit]', form);
    if (btn) btn.disabled = true;
    fetch(url, {
      method: method,
      headers: { 'X-Twd-UI': '1', 'Accept': 'application/json' },
      cache: 'no-store',
      credentials: 'same-origin'
    }).then(function (res) {
      return res.text().then(function (body) {
        var json = null;
        try { json = body ? JSON.parse(body) : null; } catch (e) { /* not JSON */ }
        if (res.ok) {
          var n = json && Array.isArray(json.killed) ? json.killed.length + ' killed' : (res.status === 204 ? 'done' : 'accepted');
          toast('ok', method + ' ' + res.status, n);
          setTimeout(function () { location.assign(after); }, 600);
          return;
        }
        var code = (json && json.code) || ('http_' + res.status);
        var text = (json && json.message) || body || res.statusText;
        toast('bad', code + ':', text, json && json.doc_url);
        if (btn) btn.disabled = false;
      });
    }).catch(function (e) {
      toast('bad', 'network_error:', e.message);
      if (btn) btn.disabled = false;
    });
  });
})();
