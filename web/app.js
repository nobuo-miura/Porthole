// Protocol card switching
const tabs = document.querySelectorAll('.proto-btn');
const typeInput = document.getElementById('type-input');

tabs.forEach(btn => {
  btn.addEventListener('click', () => {
    tabs.forEach(t => t.classList.remove('active'));
    btn.classList.add('active');
    const type = btn.dataset.type;
    typeInput.value = type;

    // Show/hide field groups
    document.querySelectorAll('.fields-group').forEach(g => g.classList.add('hidden'));
    const group = document.getElementById('fields-' + (type === 'udp' ? 'tcp' : type));
    if (group) group.classList.remove('hidden');
  });
});

// Build request object from current form state
function buildRequest() {
  const type = typeInput.value;
  const timeout = parseInt(document.getElementById('timeout-input').value) || 5;

  const get = (name) => {
    const el = document.querySelector(`[name="${name}"]`);
    return el ? el.value.trim() : '';
  };

  const req = { type, timeout_sec: timeout };

  if (type === 'tcp' || type === 'udp') {
    req.host = get('host');
    req.port = parseInt(get('port')) || 0;
  } else if (type === 'mysql' || type === 'mariadb') {
    req.uri = get('uri_mysql');
    req.host = get('host_mysql');
    req.port = parseInt(get('port_mysql')) || 3306;
    req.username = get('username_mysql');
    req.password = get('password_mysql');
    req.database = get('database_mysql');
    req.ssl_mode = get('ssl_mode_mysql');
  } else if (type === 'postgres') {
    req.uri = get('uri_postgres');
    req.host = get('host_postgres');
    req.port = parseInt(get('port_postgres')) || 5432;
    req.username = get('username_postgres');
    req.password = get('password_postgres');
    req.database = get('database_postgres');
    req.ssl_mode = get('ssl_mode_postgres');
  } else if (type === 'mongodb') {
    req.uri = get('uri_mongodb');
    req.host = get('host_mongodb');
    req.port = parseInt(get('port_mongodb')) || 27017;
    req.username = get('username_mongodb');
    req.password = get('password_mongodb');
    req.ssl_mode = get('ssl_mode_mongodb');
  } else if (type === 'redis') {
    req.uri = get('uri_redis');
    req.host = get('host_redis');
    req.port = parseInt(get('port_redis')) || 6379;
    req.password = get('password_redis');
    req.ssl_mode = get('ssl_mode_redis');
  } else if (type === 'elasticsearch') {
    req.uri = get('uri_elasticsearch');
    req.host = get('host_elasticsearch');
    req.port = parseInt(get('port_elasticsearch')) || 9200;
    req.username = get('username_elasticsearch');
    req.password = get('password_elasticsearch');
  } else if (type === 'rabbitmq') {
    req.uri = get('uri_rabbitmq');
    req.host = get('host_rabbitmq');
    req.port = parseInt(get('port_rabbitmq')) || 5672;
    req.username = get('username_rabbitmq');
    req.password = get('password_rabbitmq');
  } else if (type === 'smtp') {
    req.host = get('host_smtp');
    req.port = parseInt(get('port_smtp')) || 25;
  } else if (type === 'sqlserver' || type === 'mssql') {
    req.uri = get('uri_sqlserver');
    req.host = get('host_sqlserver');
    req.port = parseInt(get('port_sqlserver')) || 1433;
    req.username = get('username_sqlserver');
    req.password = get('password_sqlserver');
    req.database = get('database_sqlserver');
    req.ssl_mode = get('ssl_mode_sqlserver');
  }

  return req;
}

// Render a result card
function renderResult(result, compact = false) {
  const ok = result.success;
  const borderColor = ok ? 'border-green-700' : 'border-red-800';
  const badgeBg = ok ? 'bg-green-900 text-green-300' : 'bg-red-900 text-red-300';
  const badgeText = ok ? 'OK' : 'FAIL';
  const icon = ok
    ? `<svg class="w-5 h-5 text-green-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/></svg>`
    : `<svg class="w-5 h-5 text-red-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/></svg>`;

  const target = result.host
    ? `${result.host}${result.port ? ':' + result.port : ''}`
    : result.uri || '';

  if (compact) {
    return `
      <div class="flex items-center gap-3 bg-gray-800 rounded-lg px-3 py-2 border ${borderColor} result-enter">
        ${icon}
        <span class="font-mono text-sm text-gray-200 flex-1">${escHtml(target)}</span>
        <span class="text-xs text-gray-400">${result.latency_ms}ms</span>
        <span class="text-xs px-2 py-0.5 rounded font-medium ${badgeBg}">${badgeText}</span>
        ${result.detail ? `<span class="text-xs text-gray-400 truncate max-w-xs">${escHtml(result.detail)}</span>` : ''}
        ${result.error ? `<span class="text-xs text-red-400 truncate max-w-xs">${escHtml(result.error)}</span>` : ''}
      </div>`;
  }

  return `
    <div class="bg-gray-900 rounded-xl p-4 border ${borderColor} result-enter">
      <div class="flex items-start gap-3">
        ${icon}
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2 flex-wrap">
            <span class="text-xs font-bold uppercase tracking-wider px-2 py-0.5 rounded ${badgeBg}">${badgeText}</span>
            <span class="text-xs text-gray-400 uppercase">${escHtml(result.type)}</span>
            <span class="font-mono text-sm text-gray-200">${escHtml(target)}</span>
            <span class="text-xs text-gray-500 ml-auto">${result.latency_ms}ms</span>
          </div>
          ${result.detail ? `<p class="text-sm text-green-300 mt-1">${escHtml(result.detail)}</p>` : ''}
          ${result.error ? `<p class="text-sm text-red-400 mt-1 font-mono">${escHtml(result.error)}</p>` : ''}
        </div>
      </div>
    </div>`;
}

function escHtml(str) {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

// Form submission
const form = document.getElementById('check-form');
const resultArea = document.getElementById('result-area');
const submitBtn = document.getElementById('submit-btn');
const btnSpinner = document.getElementById('btn-spinner');
const btnText = document.getElementById('btn-text');

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  const req = buildRequest();

  // Basic validation (no required attrs on inputs to avoid hidden-field errors)
  if (!req.host && !req.uri) {
    resultArea.innerHTML = `<div class="bg-yellow-950 border border-yellow-700 rounded-xl p-4 text-yellow-300 text-sm">Host or URI is required.</div>`;
    return;
  }
  if ((req.type === 'tcp' || req.type === 'udp') && !req.port) {
    resultArea.innerHTML = `<div class="bg-yellow-950 border border-yellow-700 rounded-xl p-4 text-yellow-300 text-sm">Port is required for ${req.type.toUpperCase()}.</div>`;
    return;
  }

  // Loading state
  submitBtn.disabled = true;
  btnSpinner.classList.remove('hidden');
  btnText.textContent = 'Testing...';
  resultArea.innerHTML = '';

  try {
    const res = await fetch('/api/check', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    });
    const data = await res.json();
    if (res.ok) {
      resultArea.innerHTML = renderResult(data);
      addToLocalHistory(data);
      refreshHistory();
    } else {
      resultArea.innerHTML = `<div class="bg-red-950 border border-red-800 rounded-xl p-4 text-red-300 text-sm">${escHtml(data.error || 'Unknown error')}</div>`;
    }
  } catch (err) {
    resultArea.innerHTML = `<div class="bg-red-950 border border-red-800 rounded-xl p-4 text-red-300 text-sm">Request failed: ${escHtml(err.message)}</div>`;
  } finally {
    submitBtn.disabled = false;
    btnSpinner.classList.add('hidden');
    btnText.textContent = 'Test Connection';
  }
});

// Batch checker
const batchBtn = document.getElementById('batch-btn');
const batchInput = document.getElementById('batch-input');
const batchResults = document.getElementById('batch-results');

batchBtn.addEventListener('click', async () => {
  const lines = batchInput.value.split('\n').map(l => l.trim()).filter(Boolean);
  if (!lines.length) return;

  const checks = lines.map(line => {
    const parts = line.split(':');
    const port = parts.length > 1 ? parseInt(parts[parts.length - 1]) : 80;
    const host = parts.slice(0, -1).join(':') || parts[0];
    return { type: 'tcp', host, port, timeout_sec: 5 };
  });

  batchBtn.disabled = true;
  batchBtn.textContent = 'Testing...';
  batchResults.innerHTML = '<p class="text-gray-500 text-sm">Running checks...</p>';

  try {
    const res = await fetch('/api/check/batch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ checks }),
    });
    const data = await res.json();
    batchResults.innerHTML = (data.results || []).map(r => renderResult(r, true)).join('');
    (data.results || []).forEach(r => addToLocalHistory(r));
    refreshHistory();
  } catch (err) {
    batchResults.innerHTML = `<p class="text-red-400 text-sm">Error: ${escHtml(err.message)}</p>`;
  } finally {
    batchBtn.disabled = false;
    batchBtn.textContent = 'Test All';
  }
});

// --- Local history (in-memory, mirrors server) ---
let localHistory = [];

function addToLocalHistory(result) {
  localHistory.unshift(result);
  if (localHistory.length > 50) localHistory = localHistory.slice(0, 50);
}

function refreshHistory() {
  const historyList = document.getElementById('history-list');
  if (!localHistory.length) {
    historyList.innerHTML = '<p class="text-gray-500 text-sm">No checks yet.</p>';
    return;
  }
  historyList.innerHTML = localHistory.map(r => {
    const ok = r.success;
    const dot = ok
      ? '<span class="w-2 h-2 rounded-full bg-green-500 inline-block shrink-0"></span>'
      : '<span class="w-2 h-2 rounded-full bg-red-500 inline-block shrink-0"></span>';
    const target = r.host ? `${r.host}${r.port ? ':' + r.port : ''}` : '';
    const time = r.checked_at ? new Date(r.checked_at).toLocaleTimeString() : '';
    return `
      <div class="flex items-center gap-2 text-xs py-1 border-b border-gray-800">
        ${dot}
        <span class="text-gray-500 uppercase w-20 shrink-0">${escHtml(r.type)}</span>
        <span class="font-mono text-gray-300 flex-1 truncate">${escHtml(target)}</span>
        <span class="text-gray-500 shrink-0">${r.latency_ms}ms</span>
        <span class="text-gray-600 shrink-0">${time}</span>
      </div>`;
  }).join('');
}

// Clear history
document.getElementById('clear-history-btn').addEventListener('click', () => {
  localHistory = [];
  refreshHistory();
});

// Load history from server on page load
async function loadHistory() {
  try {
    const res = await fetch('/api/history');
    if (res.ok) {
      const data = await res.json();
      localHistory = (data.checks || []).reverse();
      refreshHistory();
    }
  } catch (_) {}
}

loadHistory();
