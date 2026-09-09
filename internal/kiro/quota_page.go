package kiro

import (
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func quotaPageResponse() pluginapi.ManagementResponse {
	return pluginapi.ManagementResponse{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Type":            []string{"text/html; charset=utf-8"},
			"Content-Security-Policy": []string{"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'none'; img-src 'none'; frame-ancestors 'self'"},
			"Referrer-Policy":         []string{"no-referrer"},
			"X-Content-Type-Options":  []string{"nosniff"},
		},
		Body: []byte(quotaPageHTML),
	}
}

const quotaPageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Kiro Quota</title>
<style>
:root{color-scheme:light dark;font-family:Inter,ui-sans-serif,system-ui,-apple-system,sans-serif;--bg:#f7f8fa;--card:#fff;--text:#18181b;--muted:#71717a;--line:#e4e4e7;--accent:#7c3aed;--track:#ececf1;--danger:#dc2626}
@media(prefers-color-scheme:dark){:root{--bg:#101114;--card:#18191d;--text:#f4f4f5;--muted:#a1a1aa;--line:#303136;--accent:#a78bfa;--track:#2a2b30;--danger:#f87171}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text)}main{max-width:1100px;margin:auto;padding:28px 24px 48px}.head{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:24px}h1{font-size:24px;margin:0}.sub{color:var(--muted);font-size:13px;margin-top:5px}button{border:1px solid var(--line);background:var(--card);color:var(--text);border-radius:7px;padding:9px 14px;font:inherit;font-size:13px;cursor:pointer}button:hover{border-color:var(--accent)}button:disabled{opacity:.55;cursor:default}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:14px}.card{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:17px}.card-head{display:flex;justify-content:space-between;gap:12px;align-items:flex-start}.name{font-weight:650;overflow-wrap:anywhere}.email,.meta,.reset{color:var(--muted);font-size:12px}.plan{font-size:11px;border:1px solid var(--line);border-radius:99px;padding:4px 8px;white-space:nowrap}.row{margin-top:18px}.row-top{display:flex;justify-content:space-between;gap:12px;font-size:13px}.value{font-variant-numeric:tabular-nums}.track{height:7px;background:var(--track);border-radius:99px;overflow:hidden;margin:8px 0 6px}.fill{height:100%;background:var(--accent);border-radius:inherit}.state{border:1px dashed var(--line);border-radius:8px;padding:28px;color:var(--muted);text-align:center}.error{color:var(--danger)}
</style>
</head>
<body>
<main>
  <div class="head"><div><h1>Kiro Quota</h1><div class="sub">Subscription credits across Kiro credentials</div></div><button id="refresh">Refresh</button></div>
  <div id="content" class="state">Loading quota...</div>
</main>
<script>
(() => {
  const REQUEST = 'cliproxy:plugin-management-request';
  const RESPONSE = 'cliproxy:plugin-management-response';
  const content = document.getElementById('content');
  const refresh = document.getElementById('refresh');
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const number = value => Number.isFinite(Number(value)) ? Number(value) : 0;
  const displayNumber = value => new Intl.NumberFormat(undefined,{maximumFractionDigits:2}).format(number(value));
  const displayDate = value => {
    const n = number(value);
    if (!n) return '';
    const ms = n < 1e12 ? n * 1000 : n;
    return new Intl.DateTimeFormat(undefined,{dateStyle:'medium',timeStyle:'short'}).format(new Date(ms));
  };
  const request = () => new Promise((resolve,reject) => {
    const requestId = 'kiro-' + Date.now() + '-' + Math.random().toString(16).slice(2);
    const timer = setTimeout(() => { window.removeEventListener('message', onMessage); reject(new Error('Management Center plugin bridge unavailable')); }, 15000);
    const onMessage = event => {
      const data = event.data;
      if (event.source !== parent || !data || data.type !== RESPONSE || data.requestId !== requestId) return;
      clearTimeout(timer); window.removeEventListener('message', onMessage);
      if (data.status < 200 || data.status >= 300) return reject(new Error('Quota request failed (HTTP ' + data.status + ')'));
      try { resolve(JSON.parse(data.body)); } catch (_) { reject(new Error('Quota response was not valid JSON')); }
    };
    window.addEventListener('message', onMessage);
    parent.postMessage({type:REQUEST,version:1,requestId,method:'GET',path:'/v0/management/plugins/kiro/usage',headers:{accept:'application/json'}}, '*');
  });
  const rowsFor = usage => Array.isArray(usage?.usageBreakdownList) ? usage.usageBreakdownList : [];
  const render = report => {
    const credentials = Array.isArray(report?.credentials) ? report.credentials : [];
    if (!credentials.length) { content.className='state'; content.textContent='No Kiro credentials found.'; return; }
    content.className='grid';
    content.innerHTML = credentials.map(credential => {
      if (credential.error) return '<article class="card"><div class="name">'+esc(credential.name)+'</div><p class="error">'+esc(credential.error)+'</p></article>';
      const usage = credential.usage || {};
      const plan = usage.subscriptionInfo?.subscriptionTitle || usage.subscriptionInfo?.type || 'Kiro';
      const rows = rowsFor(usage);
      const quotaRows = rows.length ? rows.map(row => {
        const used = number(row.currentUsageWithPrecision ?? row.currentUsage);
        const limit = number(row.usageLimitWithPrecision ?? row.usageLimit);
        const percent = limit > 0 ? Math.min(100, Math.max(0, used / limit * 100)) : 0;
        const label = row.displayNamePlural || row.displayName || row.resourceType || 'Usage';
        const resetAt = row.nextDateReset || usage.nextDateReset;
        return '<div class="row"><div class="row-top"><span>'+esc(label)+'</span><span class="value">'+displayNumber(used)+' / '+displayNumber(limit)+'</span></div><div class="track"><div class="fill" style="width:'+percent.toFixed(2)+'%"></div></div><div class="reset">'+(resetAt?'Resets '+esc(displayDate(resetAt)):'')+'</div></div>';
      }).join('') : '<div class="row meta">No metered usage returned.</div>';
      return '<article class="card"><div class="card-head"><div><div class="name">'+esc(credential.name)+'</div><div class="email">'+esc(credential.email || credential.authMethod || '')+'</div></div><span class="plan">'+esc(plan)+'</span></div>'+quotaRows+'</article>';
    }).join('');
  };
  const load = async () => {
    refresh.disabled=true; content.className='state'; content.textContent='Loading quota...';
    try { render(await request()); }
    catch (error) { content.className='state error'; content.textContent=error.message + '. This page requires Management Center PR #413 or a release containing its plugin bridge.'; }
    finally { refresh.disabled=false; }
  };
  refresh.addEventListener('click', load); load();
})();
</script>
</body>
</html>`
