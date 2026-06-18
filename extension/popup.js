// Stackly Chrome Extension — Popup Script

const CATEGORY_LABELS = {
  cms: 'CMS',
  framework: 'Framework',
  javascript: 'JavaScript',
  css: 'CSS',
  server: 'Server',
  hosting: 'Hosting',
  cdn: 'CDN',
  database: 'Database',
  analytics: 'Analytics',
  payment: 'Payment',
  security: 'Security',
  helpdesk: 'Helpdesk',
  fonts: 'Fonts',
  ecommerce: 'E-commerce',
  api: 'API',
  'build-tool': 'Build Tool',
  programming: 'Programming',
};

const STORAGE_KEY = 'stackly_api_url';
const DEFAULT_API = 'http://localhost:8890';

// DOM
const urlEl = document.getElementById('url');
const scanBtn = document.getElementById('scan-btn');
const btnLabel = document.getElementById('btn-label');
const apiInput = document.getElementById('api-url');
const statusEl = document.getElementById('status');
const resultsEl = document.getElementById('results');
const openWeb = document.getElementById('open-web');

let currentTab = null;

// ===== Init =====
chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
  if (tabs && tabs[0]) {
    currentTab = tabs[0];
    urlEl.textContent = currentTab.url || 'No URL';
  }
});

chrome.storage.sync.get([STORAGE_KEY], (data) => {
  apiInput.value = data[STORAGE_KEY] || DEFAULT_API;
  openWeb.href = apiInput.value || DEFAULT_API;
});

apiInput.addEventListener('change', () => {
  const url = apiInput.value.trim() || DEFAULT_API;
  chrome.storage.sync.set({ [STORAGE_KEY]: url });
  openWeb.href = url;
});

scanBtn.addEventListener('click', () => {
  if (!currentTab || !currentTab.url) {
    setStatus('No active tab URL', 'error');
    return;
  }
  scan(currentTab.url);
});

// ===== Scan =====

async function scan(url) {
  const apiUrl = apiInput.value.trim() || DEFAULT_API;

  // UI: loading
  setLoading(true);
  setStatus('Scanning...', 'info');
  resultsEl.innerHTML = '';

  try {
    // Submit scan with wait=true
    const response = await fetch(`${apiUrl}/api/scan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, wait: true, timeout: 60 }),
    });

    if (!response.ok) {
      const err = await response.json().catch(() => ({}));
      throw new Error(err.error || `HTTP ${response.status}`);
    }

    const job = await response.json();
    const detail = await fetch(`${apiUrl}/api/results/${job.id}`).then(r => r.json());

    renderResults(detail);
    setStatus(`Done in ${detail.duration || '0s'}${detail.from_cache ? ' (cached)' : ''}`, 'success');
  } catch (err) {
    setStatus(`Error: ${err.message}`, 'error');
    resultsEl.innerHTML = `<div style="padding: 16px; text-align: center; color: var(--text-dim); font-size: 12px;">Cannot reach API at ${apiUrl}</div>`;
  } finally {
    setLoading(false);
  }
}

function setLoading(loading) {
  scanBtn.disabled = loading;
  btnLabel.textContent = loading ? 'Scanning...' : 'Detect Stack';
}

function setStatus(text, type) {
  statusEl.textContent = text;
  statusEl.className = `status active status-${type}`;
  statusEl.style.color = type === 'error' ? '#F87171' : type === 'success' ? '#22C55E' : '#94A3B8';
}

function renderResults(job) {
  if (job.status !== 'completed' || !job.result) {
    resultsEl.innerHTML = `<div style="padding: 16px; text-align: center; color: #F87171;">${job.error || 'Scan failed'}</div>`;
    return;
  }

  const detections = job.result.Results || [];
  if (detections.length === 0) {
    resultsEl.innerHTML = '<div style="padding: 16px; text-align: center; color: var(--text-dim); font-size: 12px;">No technologies detected</div>';
    return;
  }

  // Group by category
  const grouped = {};
  for (const d of detections) {
    const cat = d.Technology.category;
    if (!grouped[cat]) grouped[cat] = [];
    grouped[cat].push(d);
  }

  let html = '';
  // Status pill at top
  html += `
    <div style="margin-bottom: 12px; padding: 8px 10px; background: var(--bg-surface); border-radius: 6px; border: 1px solid var(--border);">
      <div style="display: flex; justify-content: space-between; font-size: 11px;">
        <span style="color: var(--accent);">● ${detections.length} detected</span>
        <span style="color: var(--text-dim); font-family: ui-monospace, monospace;">${job.duration || ''}</span>
      </div>
    </div>
  `;

  for (const cat of Object.keys(grouped)) {
    const techs = grouped[cat];
    const label = CATEGORY_LABELS[cat] || cat;
    html += `
      <div class="category">
        <div class="cat-header cat-${cat}">
          <span>${label}</span>
          <span class="cat-count">${techs.length}</span>
        </div>
        <div>
          ${techs.map(t => `<span class="tech-pill">${escapeHtml(t.Technology.name)}</span>`).join('')}
        </div>
      </div>
    `;
  }

  // Implied
  const implied = [];
  const detected = new Set(detections.map(d => d.Technology.slug));
  for (const d of detections) {
    if (d.Technology.implies) {
      for (const imp of d.Technology.implies) {
        if (!detected.has(imp) && !implied.includes(imp)) implied.push(imp);
      }
    }
  }
  if (implied.length > 0) {
    html += `
      <div style="margin-top: 12px; padding: 10px; background: var(--bg-surface); border-radius: 6px; border: 1px solid var(--border);">
        <div style="font-size: 11px; color: var(--text-dim); text-transform: uppercase; margin-bottom: 6px;">Implied</div>
        <div>${implied.map(s => `<span class="tech-pill" style="color: #22D3EE;">→ ${escapeHtml(s)}</span>`).join('')}</div>
      </div>
    `;
  }

  resultsEl.innerHTML = html;
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}