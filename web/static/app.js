// Stackly — Tech Stack Detector (frontend)
// Loaded icon manifest: /static/icons.json (devicon) + /static/inline-icons.json (fallback)

const API_BASE = window.location.origin;
const STORAGE_KEY = 'stackly_recent';
const AUTH_STORAGE_KEY = 'stackly_auth';

// State
let currentJobId = null;
let pollInterval = null;
let ICON_MANIFEST = {};      // { slug: { dir, variant, url } }
let INLINE_ICONS = {};       // { slug: [letters, color] }

// ===== Icon resolution =====
function iconUrlFor(slug) {
  const m = ICON_MANIFEST[slug];
  if (m && m.url) return { type: 'img', url: m.url };
  const inline = INLINE_ICONS[slug];
  if (inline) return { type: 'inline', letters: inline[0], color: inline[1] };
  // Letter fallback from name
  return { type: 'fallback', letter: (slug[0] || '?').toUpperCase() };
}

function techIconHTML(slug, name, size = 'sm') {
  const icon = iconUrlFor(slug);
  const cls = size === 'lg' ? 'tech-icon-lg' : 'tech-icon';
  if (icon.type === 'img') {
    return `<img src="${icon.url}" class="${cls}" alt="${name}" loading="lazy" onerror="this.replaceWith(makeFallbackLetter('${slug}','${name}',${size === 'lg' ? 32 : 18}))">`;
  }
  return fallbackLetterHTML(slug, name, size === 'lg' ? 32 : 18);
}

function fallbackLetterHTML(slug, name, size) {
  const icon = iconUrlFor(slug);
  const fontSize = Math.max(10, Math.floor(size * 0.45));
  const letters = icon.letters || icon.letter || (name ? name[0] : '?').toUpperCase();
  const color = icon.color || '#22C55E';
  return `<span class="${size >= 32 ? 'tech-icon-lg' : 'tech-icon'}" style="display:inline-flex;align-items:center;justify-content:center;border-radius:6px;background:${color}22;color:${color};font-weight:700;font-size:${fontSize}px;line-height:1;font-family:'Space Grotesk',system-ui,sans-serif;flex-shrink:0">${letters}</span>`;
}

// Make fallback available globally for onerror handlers
window.makeFallbackLetter = (slug, name, size) => {
  const div = document.createElement('span');
  div.outerHTML = fallbackLetterHTML(slug, name, size);
  return div;
};

// ===== Fingerprint map (slug → {name, website, category}) =====
// Loaded once on init from /api/fingerprints. Used to enrich implied tech
// pills (which aren't in the scan response directly) with clickable website links.
let fingerprintsMap = {};

async function loadFingerprintsMap() {
  try {
    const res = await fetch(`${API_BASE}/api/fingerprints`);
    if (res.ok) fingerprintsMap = await res.json();
  } catch (e) {
    console.warn('Fingerprints map failed to load', e);
  }
}

// ===== DOM refs =====
const form = document.getElementById('scan-form');
const input = document.getElementById('url-input');
const scanBtn = document.getElementById('scan-btn');
const scanBtnLabel = document.getElementById('scan-btn-label');
const scanBtnIcon = document.getElementById('scan-btn-icon');
const scanBtnSpinner = document.getElementById('scan-btn-spinner');

const resultsSection = document.getElementById('results-section');
const recentSection = document.getElementById('recent-section');
const recentList = document.getElementById('recent-list');

const resultStatus = document.getElementById('result-status');
const resultDuration = document.getElementById('result-duration');
const resultUrl = document.getElementById('result-url');
const categoriesContainer = document.getElementById('categories-container');
const summaryBanner = document.getElementById('summary-banner');
const marketingHighlight = document.getElementById('marketing-highlight');
const impliedContainer = document.getElementById('implied-container');
const impliedList = document.getElementById('implied-list');
const emailsContainer = document.getElementById('emails-container');
const emailsList = document.getElementById('emails-list');
const emailsCount = document.getElementById('emails-count');
const copyJsonBtn = document.getElementById('copy-json-btn');

const emptyState = document.getElementById('empty-state');
const errorState = document.getElementById('error-state');
const errorMessage = document.getElementById('error-message');
const loadingState = document.getElementById('loading-state');
const loadingTarget = document.getElementById('loading-target');

const statusIndicator = document.getElementById('status-indicator');
const queueStatsEl = document.getElementById('queue-stats');

// Auth UI
const authModal = document.getElementById('auth-modal');
const authBtn = document.getElementById('open-auth-btn');
const authBtnLabel = document.getElementById('auth-btn-label');
const closeAuthBtn = document.getElementById('close-auth-btn');
const authForm = document.getElementById('auth-form');
const authSubmitBtn = document.getElementById('auth-submit-btn');
const authClearBtn = document.getElementById('auth-clear-btn');
const authStatus = document.getElementById('auth-status');
const apikeyInput = document.getElementById('apikey-input');
const basicUserInput = document.getElementById('basic-user-input');
const basicPassInput = document.getElementById('basic-pass-input');
const jwtInput = document.getElementById('jwt-input');

// Install guide
const installModal = document.getElementById('install-modal');
const openInstallBtn = document.getElementById('open-install-help-btn');
const openExtensionBtn = document.getElementById('open-extension-btn');
const closeInstallBtn = document.getElementById('close-install-btn');

// Category meta — individual category colors (used for category pills, implied-tech badges)
const CATEGORY_COLORS = {
  advertising:    { bg: 'bg-gradient-to-br from-orange-500/15 to-pink-500/10', border: 'border-orange-500/40', text: 'text-orange-300', icon: 'text-orange-400', glow: 'shadow-orange-500/10' },
  'tag-managers': { bg: 'bg-purple-500/10', border: 'border-purple-500/30', text: 'text-purple-300', icon: 'text-purple-400' },
  analytics:      { bg: 'bg-blue-500/10', border: 'border-blue-500/30', text: 'text-blue-300', icon: 'text-blue-400' },
  seo:            { bg: 'bg-blue-500/10', border: 'border-blue-500/30', text: 'text-blue-300', icon: 'text-blue-400' },
  performance:    { bg: 'bg-emerald-500/10', border: 'border-emerald-500/30', text: 'text-emerald-300', icon: 'text-emerald-400' },
  'cookie-compliance': { bg: 'bg-amber-500/10', border: 'border-amber-500/30', text: 'text-amber-300', icon: 'text-amber-400' },
  'live-chat':    { bg: 'bg-gradient-to-br from-teal-500/15 to-cyan-500/10', border: 'border-teal-500/40', text: 'text-teal-300', icon: 'text-teal-400', glow: 'shadow-teal-500/10' },
  cms:            { bg: 'bg-emerald-500/10', border: 'border-emerald-500/30', text: 'text-emerald-300', icon: 'text-emerald-400' },
  framework:      { bg: 'bg-cyan-500/10', border: 'border-cyan-500/30', text: 'text-cyan-300', icon: 'text-cyan-400' },
  javascript:     { bg: 'bg-yellow-500/10', border: 'border-yellow-500/30', text: 'text-yellow-300', icon: 'text-yellow-400' },
  css:            { bg: 'bg-pink-500/10', border: 'border-pink-500/30', text: 'text-pink-300', icon: 'text-pink-400' },
  'build-tool':   { bg: 'bg-rose-500/10', border: 'border-rose-500/30', text: 'text-rose-300', icon: 'text-rose-400' },
  ecommerce:      { bg: 'bg-green-500/10', border: 'border-green-500/30', text: 'text-green-300', icon: 'text-green-400' },
  server:         { bg: 'bg-slate-500/15', border: 'border-slate-500/40', text: 'text-slate-200', icon: 'text-slate-300' },
  hosting:        { bg: 'bg-violet-500/10', border: 'border-violet-500/30', text: 'text-violet-300', icon: 'text-violet-400' },
  cdn:            { bg: 'bg-orange-500/10', border: 'border-orange-500/30', text: 'text-orange-300', icon: 'text-orange-400' },
  database:       { bg: 'bg-indigo-500/10', border: 'border-indigo-500/30', text: 'text-indigo-300', icon: 'text-indigo-400' },
  programming:    { bg: 'bg-sky-500/10', border: 'border-sky-500/30', text: 'text-sky-300', icon: 'text-sky-400' },
  fonts:          { bg: 'bg-amber-500/10', border: 'border-amber-500/30', text: 'text-amber-300', icon: 'text-amber-400' },
  payment:        { bg: 'bg-lime-500/10', border: 'border-lime-500/30', text: 'text-lime-300', icon: 'text-lime-400' },
  security:       { bg: 'bg-red-500/10', border: 'border-red-500/30', text: 'text-red-300', icon: 'text-red-400' },
  api:            { bg: 'bg-purple-500/10', border: 'border-purple-500/30', text: 'text-purple-300', icon: 'text-purple-400' },
  maps:           { bg: 'bg-rose-500/10', border: 'border-rose-500/30', text: 'text-rose-300', icon: 'text-rose-400' },
};

const CATEGORY_LABELS = {
  advertising:    'Advertising & Marketing',
  'tag-managers': 'Tag Managers',
  analytics:      'Analytics',
  seo:            'SEO',
  performance:    'Performance & RUM',
  'cookie-compliance': 'Cookie Compliance',
  'live-chat':    'Live Chat',
  cms:            'Content Management',
  framework:      'Web Frameworks',
  javascript:     'JavaScript Libraries',
  css:            'CSS Frameworks',
  'build-tool':   'Build Tools',
  ecommerce:      'E-commerce',
  server:         'Web Servers',
  hosting:        'Hosting & PaaS',
  cdn:            'CDN & Edge',
  database:       'Databases',
  programming:    'Programming Languages',
  fonts:          'Font Scripts',
  payment:        'Payment',
  security:       'Security',
  api:            'API & GraphQL',
  maps:           'Maps',
};

// Map each raw category to its Wappalyzer top-level group
const CATEGORY_GROUP = {
  advertising:    'advertising',
  'tag-managers': 'tag-managers',
  analytics:      'analytics',
  seo:            'analytics',
  performance:    'performance',
  'cookie-compliance': 'cookie-compliance',
  'live-chat':    'live-chat',
  javascript:     'javascript',
  framework:      'frameworks',
  'build-tool':   'frameworks',
  css:            'frameworks',
  cms:            'cms',
  ecommerce:      'ecommerce',
  cdn:            'cdn',
  hosting:        'hosting',
  server:         'hosting',
  database:       'database',
  programming:    'languages',
  api:            'languages',
  fonts:          'fonts',
  security:       'security',
  payment:        'payment',
  maps:           'maps',
};

// Top-level group order (Wappalyzer priority: marketing signals first, then infra, then stack, then aux)
const GROUP_ORDER = ['advertising', 'tag-managers', 'analytics', 'performance', 'cookie-compliance', 'live-chat', 'javascript', 'frameworks', 'cms', 'ecommerce', 'cdn', 'hosting', 'database', 'languages', 'fonts', 'security', 'payment', 'maps'];

// Sub-category priority (Wappalyzer-style: marketing → infrastructure → stack → misc)
const CATEGORY_ORDER = ['advertising', 'tag-managers', 'analytics', 'seo', 'performance', 'cookie-compliance', 'live-chat', 'javascript', 'framework', 'build-tool', 'css', 'cms', 'ecommerce', 'cdn', 'hosting', 'server', 'database', 'programming', 'api', 'fonts', 'security', 'payment', 'maps'];

// Top-level group meta — label, accent color, optional gradient (highlight groups)
const GROUP_META = {
  advertising:         { label: 'Advertising',     accent: 'text-orange-300',  dot: 'bg-orange-400',  gradient: true },
  'tag-managers':      { label: 'Tag Managers',    accent: 'text-purple-300',  dot: 'bg-purple-400' },
  analytics:           { label: 'Analytics & SEO', accent: 'text-blue-300',    dot: 'bg-blue-400' },
  performance:         { label: 'Performance',     accent: 'text-emerald-300', dot: 'bg-emerald-400' },
  'cookie-compliance': { label: 'Cookie Compliance', accent: 'text-amber-300', dot: 'bg-amber-400' },
  'live-chat':         { label: 'Live Chat',       accent: 'text-teal-300',    dot: 'bg-teal-400',   gradient: true },
  javascript:          { label: 'JavaScript Libraries', accent: 'text-yellow-300', dot: 'bg-yellow-400' },
  frameworks:          { label: 'Frameworks & Build Tools', accent: 'text-cyan-300', dot: 'bg-cyan-400' },
  cms:                 { label: 'CMS',             accent: 'text-emerald-300', dot: 'bg-emerald-400' },
  ecommerce:           { label: 'E-commerce',      accent: 'text-green-300',   dot: 'bg-green-400' },
  cdn:                 { label: 'CDN & Edge',      accent: 'text-orange-300',  dot: 'bg-orange-400' },
  hosting:             { label: 'Hosting & Servers', accent: 'text-violet-300', dot: 'bg-violet-400' },
  database:            { label: 'Databases',       accent: 'text-indigo-300',  dot: 'bg-indigo-400' },
  languages:           { label: 'Languages & APIs', accent: 'text-sky-300',    dot: 'bg-sky-400' },
  fonts:               { label: 'Font Scripts',    accent: 'text-amber-300',   dot: 'bg-amber-400' },
  security:            { label: 'Security',        accent: 'text-red-300',     dot: 'bg-red-400' },
  payment:             { label: 'Payment',         accent: 'text-lime-300',    dot: 'bg-lime-400' },
  maps:                { label: 'Maps',            accent: 'text-rose-300',    dot: 'bg-rose-400' },
};

// Categories that get pulled into the marketing highlight section
const MARKETING_CATEGORIES = new Set(['advertising', 'tag-managers']);

// ===== Auth =====
function getAuth() {
  try {
    const raw = localStorage.getItem(AUTH_STORAGE_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch { return null; }
}

function setAuth(auth) {
  if (auth) localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(auth));
  else localStorage.removeItem(AUTH_STORAGE_KEY);
  renderAuthBtn();
}

function renderAuthBtn() {
  const auth = getAuth();
  if (auth) {
    authBtnLabel.textContent = auth.user || 'Signed in';
    authBtn.classList.remove('bg-accent/10', 'border-accent/30', 'text-accent');
    authBtn.classList.add('bg-emerald-500/10', 'border-emerald-500/30', 'text-emerald-400');
  } else {
    authBtnLabel.textContent = 'Sign in';
    authBtn.classList.add('bg-accent/10', 'border-accent/30', 'text-accent');
    authBtn.classList.remove('bg-emerald-500/10', 'border-emerald-500/30', 'text-emerald-400');
  }
}

function authHeaders() {
  const auth = getAuth();
  if (!auth) return {};
  if (auth.type === 'apikey') {
    if (auth.scheme === 'bearer') return { 'Authorization': `Bearer ${auth.token}` };
    return { 'X-API-Key': auth.token };
  }
  if (auth.type === 'basic') {
    return { 'Authorization': 'Basic ' + btoa(`${auth.user}:${auth.pass}`) };
  }
  if (auth.type === 'jwt') {
    return { 'Authorization': `Bearer ${auth.token}` };
  }
  return {};
}

// ===== API calls =====
async function submitScan(url) {
  if (!url) return;
  if (!/^https?:\/\//i.test(url)) url = 'https://' + url;
  input.value = url;
  setLoading(true);
  showLoadingState(url);
  resultsSection.classList.remove('hidden');

  try {
    const response = await fetch(`${API_BASE}/api/scan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...authHeaders() },
      body: JSON.stringify({ url, wait: true, timeout: 60 })
    });

    if (!response.ok) {
      const err = await response.json().catch(() => ({}));
      throw new Error(err.error || `HTTP ${response.status}`);
    }

    const job = await response.json();
    currentJobId = job.id;

    if (job.status === 'completed' || job.status === 'failed') {
      const detailRes = await fetch(`${API_BASE}/api/results/${job.id}`, { headers: authHeaders() });
      const detail = await detailRes.json();
      renderResult(detail);
      if (job.status === 'completed') addRecent(url);
    } else {
      // Try WebSocket for live progress, fall back to polling if WS fails.
      const wsOk = await subscribeToProgress(job.id, url);
      if (!wsOk) pollJob(job.id);
    }
  } catch (err) {
    // Auth failures: pop sign-in modal automatically
    if (/credentials|invalid auth|401/i.test(err.message)) {
      showAuthModal();
      showError('Authentication required — please sign in.');
    } else {
      showError(err.message);
    }
  } finally {
    setLoading(false);
  }
}

// subscribeToProgress opens a WebSocket to /api/ws/scan/:id.
// Returns true if WS handled the completion, false if caller should fall back to polling.
function subscribeToProgress(jobId, url) {
  return new Promise((resolve) => {
    let resolved = false;
    const wsScheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${wsScheme}//${window.location.host}/api/ws/scan/${jobId}`;
    let socket;

    const fallback = () => {
      if (resolved) return;
      resolved = true;
      try { socket && socket.close(); } catch {}
      resolve(false);
    };

    const onComplete = (finalResult) => {
      if (resolved) return;
      resolved = true;
      try { socket && socket.close(); } catch {}
      renderResult(finalResult);
      if (finalResult.status === 'completed') addRecent(url);
      setLoading(false);
      resolve(true);
    };

    try {
      socket = new WebSocket(wsUrl);
    } catch (e) {
      console.warn('WS construction failed:', e);
      return resolve(false);
    }

    // Bail out to polling after 8s if no events.
    const bailTimer = setTimeout(fallback, 8000);

    socket.onopen = () => {
      clearTimeout(bailTimer);
    };

    socket.onmessage = (msg) => {
      let ev;
      try { ev = JSON.parse(msg.data); } catch { return; }

      // Update progress UI from each event
      if (typeof ev.progress === 'number') {
        updateProgress(ev.progress, ev.message || ev.status || '');
      }

      if (ev.type === 'completed') {
        // Build a job-shaped object from the WS event for renderResult
        const fakeJob = {
          id: jobId,
          url,
          status: 'completed',
          duration: 'live',
          result: { Results: (ev.techs || []).map(t => ({
            Technology: { slug: t.slug, name: t.name, category: t.category },
            Confidence: t.confidence,
            Matches: [],
          })) },
        };
        onComplete(fakeJob);
      } else if (ev.type === 'failed') {
        const fakeJob = {
          id: jobId,
          url,
          status: 'failed',
          error: ev.error || 'Unknown error',
        };
        onComplete(fakeJob);
      }
    };

    socket.onerror = () => {
      clearTimeout(bailTimer);
      fallback();
    };

    socket.onclose = (e) => {
      clearTimeout(bailTimer);
      if (!resolved) fallback();
    };
  });
}

function pollJob(jobId) {
  if (pollInterval) clearInterval(pollInterval);
  pollInterval = setInterval(async () => {
    try {
      const res = await fetch(`${API_BASE}/api/results/${jobId}?wait=true`, { headers: authHeaders() });
      const job = await res.json();
      if (job.status === 'completed' || job.status === 'failed') {
        clearInterval(pollInterval);
        pollInterval = null;
        renderResult(job);
        if (job.status === 'completed') addRecent(job.url);
        setLoading(false);
      }
    } catch (err) {
      clearInterval(pollInterval);
      pollInterval = null;
      showError(err.message);
      setLoading(false);
    }
  }, 800);
}

// ===== Render =====
function setLoading(loading) {
  scanBtn.disabled = loading;
  scanBtnLabel.textContent = loading ? 'Scanning' : 'Scan';
  scanBtnIcon.classList.toggle('hidden', loading);
  scanBtnSpinner.classList.toggle('hidden', !loading);
}

function hideAllStates() {
  emptyState.classList.add('hidden');
  errorState.classList.add('hidden');
  loadingState.classList.add('hidden');
  categoriesContainer.innerHTML = '';
  summaryBanner.classList.add('hidden');
  summaryBanner.innerHTML = '';
  marketingHighlight.classList.add('hidden');
  marketingHighlight.innerHTML = '';
  impliedContainer.classList.add('hidden');
  emailsContainer.classList.add('hidden');
  // Reset progress bar
  const wrap = loadingState.querySelector('.progress-wrap');
  if (wrap) {
    wrap.querySelector('.progress-bar').style.width = '0%';
    wrap.querySelector('.progress-label').textContent = '0%';
    wrap.querySelector('.progress-message').textContent = 'Initializing…';
  }
}

function showLoadingState(url) {
  hideAllStates();
  loadingState.classList.remove('hidden');
  loadingTarget.textContent = url;
  updateProgress(0, 'Initializing...');
}

function updateProgress(percent, message) {
  const wrap = loadingState.querySelector('.progress-wrap') || createProgressWrap();
  const bar = wrap.querySelector('.progress-bar');
  const label = wrap.querySelector('.progress-label');
  const msg = wrap.querySelector('.progress-message');

  const pct = Math.max(0, Math.min(100, percent || 0));
  bar.style.width = pct + '%';
  label.textContent = pct + '%';
  if (message) msg.textContent = message;
}

function createProgressWrap() {
  const wrap = document.createElement('div');
  wrap.className = 'progress-wrap mt-5 mx-auto max-w-md';
  wrap.innerHTML = `
    <div class="flex items-center justify-between text-xs font-mono text-slate-400 mb-2">
      <span class="progress-message text-slate-300">Initializing…</span>
      <span class="progress-label text-accent">0%</span>
    </div>
    <div class="h-1.5 rounded-full bg-white/5 overflow-hidden">
      <div class="progress-bar h-full bg-gradient-to-r from-accent to-cyan-400 transition-all duration-300 ease-out" style="width:0%"></div>
    </div>
  `;
  loadingState.appendChild(wrap);
  return wrap;
}

function showError(message) {
  hideAllStates();
  errorState.classList.remove('hidden');
  errorMessage.textContent = message;
}

// ===== Result rendering helpers =====

function renderTechPill(t, opts = {}) {
  const website = t.Technology.website;
  const Tag = website ? 'a' : 'div';
  const linkAttrs = website
    ? `href="${escapeHtml(website)}" target="_blank" rel="noopener noreferrer" class="tech-pill group flex items-center gap-2 px-2.5 py-1.5 rounded-lg bg-bg-base/70 border border-white/10 hover:border-accent/50 hover:bg-bg-base/90 transition-colors min-w-0" title="Open ${escapeHtml(t.Technology.name)} website"`
    : `class="tech-pill flex items-center gap-2 px-2.5 py-1.5 rounded-lg bg-bg-base/70 border border-white/10 cursor-default min-w-0" title="Confidence: ${t.Confidence}"`;

  // Confidence badge — only show when > 100 (multiple match signals)
  const showConfidence = t.Confidence > 100;
  const confColor = t.Confidence >= 200 ? 'text-emerald-400' : t.Confidence >= 100 ? 'text-accent' : 'text-slate-500';

  return `
    <${Tag} ${linkAttrs}>
      ${techIconHTML(t.Technology.slug, t.Technology.name)}
      <div class="flex-1 min-w-0">
        <div class="text-xs sm:text-sm text-slate-100 font-medium leading-tight whitespace-nowrap overflow-hidden text-ellipsis">${escapeHtml(t.Technology.name)}</div>
        ${t.Matches && t.Matches.length > 0 ? `<div class="text-[10px] font-mono text-slate-500 leading-tight whitespace-nowrap overflow-hidden text-ellipsis">${escapeHtml(t.Matches[0].Type)}${t.Matches.length > 1 ? ' · ' + t.Matches.length + ' signals' : ''}</div>` : ''}
      </div>
      ${showConfidence ? `<span class="text-[10px] font-mono ${confColor} shrink-0">${t.Confidence}</span>` : ''}
      ${website ? `<svg class="w-3 h-3 text-slate-500 group-hover:text-accent transition-colors shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"/></svg>` : ''}
    </${Tag}>`;
}

function renderCategoryCard(cat, techs, compact = false) {
  const colors = CATEGORY_COLORS[cat] || CATEGORY_COLORS.programming;
  const label = CATEGORY_LABELS[cat] || cat;
  const sorted = techs.sort((a, b) => b.Confidence - a.Confidence);
  // Single-col pills on mobile (full width, no truncation); 2-col from sm+
  const gridCols = compact ? 'grid-cols-1 sm:grid-cols-2' : 'grid-cols-1 sm:grid-cols-2';
  // Min-height ensures sparse cards (1-2 techs) visually balance with denser siblings
  const minH = sorted.length <= 2 ? 'min-h-[88px]' : sorted.length <= 4 ? 'min-h-[120px]' : 'min-h-[160px]';

  return `
    <div class="${colors.bg} ${colors.border} border rounded-2xl p-3 sm:p-4 animate-fade-in shadow-lg ${colors.glow || ''} h-full flex flex-col ${minH}">
      <div class="flex items-center justify-between mb-2 sm:mb-3 shrink-0">
        <h3 class="font-display font-semibold text-xs sm:text-sm ${colors.text} uppercase tracking-wider truncate">${escapeHtml(label)}</h3>
        <span class="text-xs font-mono ${colors.icon} shrink-0">${sorted.length}</span>
      </div>
      <div class="grid ${gridCols} gap-1.5 flex-1 content-start">
        ${sorted.map(t => renderTechPill(t)).join('')}
      </div>
    </div>
  `;
}

function renderSummaryBanner(grouped, sortedCats) {
  const totalTechs = Object.values(grouped).reduce((s, t) => s + t.length, 0);
  const totalCats = Object.keys(grouped).length;
  const marketingCount = (grouped['advertising'] || []).length + (grouped['tag-managers'] || []).length;

  // Pull "key infrastructure" — server, hosting, cdn
  const infra = [];
  for (const cat of ['server', 'hosting', 'cdn']) {
    if (grouped[cat]) {
      for (const t of grouped[cat]) infra.push(t.Technology.name);
    }
  }

  const hasContent = totalTechs > 0;
  if (!hasContent) {
    summaryBanner.classList.add('hidden');
    return;
  }

  summaryBanner.classList.remove('hidden');
  summaryBanner.innerHTML = `
    <div class="bg-bg-surface/80 backdrop-blur border border-white/10 rounded-2xl p-4 sm:p-5 animate-fade-in">
      <div class="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-3 sm:gap-4">
        <div class="flex items-center gap-4 sm:gap-6 flex-wrap">
          <div>
            <div class="text-[10px] font-mono uppercase tracking-wider text-slate-500">Tech</div>
            <div class="font-display font-semibold text-2xl text-slate-100">${totalTechs}</div>
          </div>
          <div class="h-10 w-px bg-white/10"></div>
          <div>
            <div class="text-[10px] font-mono uppercase tracking-wider text-slate-500">Categories</div>
            <div class="font-display font-semibold text-2xl text-slate-100">${totalCats}</div>
          </div>
          ${marketingCount > 0 ? `
            <div class="h-10 w-px bg-white/10"></div>
            <div>
              <div class="text-[10px] font-mono uppercase tracking-wider text-orange-400">Marketing</div>
              <div class="font-display font-semibold text-2xl text-orange-300">${marketingCount}</div>
            </div>
          ` : ''}
        </div>
        ${infra.length > 0 ? `
          <div class="hidden sm:flex items-center gap-2 flex-wrap">
            <span class="text-[10px] font-mono uppercase tracking-wider text-slate-500">Stack:</span>
            ${infra.slice(0, 4).map((name, i) => `
              <span class="inline-flex items-center gap-1 px-2 py-1 rounded-md bg-slate-500/15 border border-slate-500/30 text-xs font-mono text-slate-200">
                ${escapeHtml(name)}${i < Math.min(infra.length - 1, 3) && i < 3 ? '<span class="text-slate-600 ml-1">→</span>' : ''}
              </span>
            `).join('')}
          </div>
        ` : ''}
      </div>
    </div>
  `;
}

function renderMarketingHighlight(grouped) {
  const advertisingTechs = grouped['advertising'] || [];
  const tagManagerTechs = grouped['tag-managers'] || [];
  const total = advertisingTechs.length + tagManagerTechs.length;

  if (total === 0) {
    marketingHighlight.classList.add('hidden');
    return;
  }

  // Build sub-sections
  const advertisingColors = CATEGORY_COLORS.advertising;
  const tagManagerColors = CATEGORY_COLORS['tag-managers'];

  let subHtml = '';
  if (advertisingTechs.length > 0) {
    subHtml += `
      <div>
        <div class="flex items-center gap-2 mb-3">
          <svg class="w-4 h-4 ${advertisingColors.icon}" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 5.882V19.24a1.76 1.76 0 01-3.417.592l-2.147-6.15M18 13a3 3 0 100-6M5.436 13.683A4.001 4.001 0 017 6h1.832c4.1 0 7.625-1.234 9.168-3v14c-1.543-1.766-5.067-3-9.168-3H7a3.988 3.988 0 01-1.564-.317z"/></svg>
          <h4 class="font-display font-semibold text-xs ${advertisingColors.text} uppercase tracking-wider">Advertising &amp; Pixels</h4>
          <span class="text-[10px] font-mono ${advertisingColors.icon}">${advertisingTechs.length}</span>
        </div>
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
          ${advertisingTechs.sort((a, b) => b.Confidence - a.Confidence).map(t => renderTechPill(t)).join('')}
        </div>
      </div>
    `;
  }
  if (tagManagerTechs.length > 0) {
    subHtml += `
      <div>
        <div class="flex items-center gap-2 mb-3">
          <svg class="w-4 h-4 ${tagManagerColors.icon}" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20.59 13.41l-7.17 7.17a2 2 0 01-2.83 0L2 12V2h10l8.59 8.59a2 2 0 010 2.82z"></path><line x1="7" y1="7" x2="7.01" y2="7"></line></svg>
          <h4 class="font-display font-semibold text-xs ${tagManagerColors.text} uppercase tracking-wider">Tag Managers</h4>
          <span class="text-[10px] font-mono ${tagManagerColors.icon}">${tagManagerTechs.length}</span>
        </div>
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
          ${tagManagerTechs.sort((a, b) => b.Confidence - a.Confidence).map(t => renderTechPill(t)).join('')}
        </div>
      </div>
    `;
  }

  marketingHighlight.classList.remove('hidden');
  marketingHighlight.innerHTML = `
    <div class="relative bg-gradient-to-br from-orange-500/10 via-pink-500/5 to-fuchsia-500/10 border border-orange-500/30 rounded-2xl p-4 sm:p-5 shadow-lg shadow-orange-500/5 animate-fade-in">
      <div class="flex items-center justify-between mb-3 sm:mb-4">
        <div class="flex items-center gap-2 min-w-0">
          <svg class="w-5 h-5 text-orange-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L2 7l10 5 10-5-10-5z"></path><polyline points="2 17 12 22 22 17"></polyline><polyline points="2 12 12 17 22 12"></polyline></svg>
          <h3 class="font-display font-semibold text-base text-orange-200 truncate">Marketing Signals</h3>
        </div>
        <span class="px-2 py-0.5 rounded-md text-[10px] font-mono uppercase tracking-wider bg-orange-500/15 border border-orange-500/30 text-orange-300 shrink-0">${total}</span>
      </div>
      <div class="grid grid-cols-1 ${total > 4 ? 'lg:grid-cols-2' : ''} gap-4 sm:gap-6">
        ${subHtml}
      </div>
    </div>
  `;
}

function renderResult(job) {
  hideAllStates();
  resultUrl.textContent = job.url;

  if (job.status === 'completed') {
    showDownloadButtons();
    refreshQuotaWidget(); // quota decremented on success
    resultStatus.className = 'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-mono font-medium bg-emerald-500/10 border border-emerald-500/30 text-emerald-400';
    const fromCache = job.from_cache ? ' · cached' : '';
    resultStatus.innerHTML = `<div class="w-1.5 h-1.5 rounded-full bg-emerald-400"></div>completed${fromCache}`;
    resultDuration.textContent = job.duration || '';
  } else if (job.status === 'failed') {
    hideDownloadButtons();
    resultStatus.className = 'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-mono font-medium bg-red-500/10 border border-red-500/30 text-red-400';
    resultStatus.innerHTML = '<div class="w-1.5 h-1.5 rounded-full bg-red-400"></div>failed';
    resultDuration.textContent = '';
    showError(job.error || 'Unknown error');
    return;
  } else {
    resultStatus.className = 'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-mono font-medium bg-amber-500/10 border border-amber-500/30 text-amber-400';
    resultStatus.innerHTML = '<div class="w-1.5 h-1.5 rounded-full bg-amber-400 animate-pulse"></div>' + job.status;
    resultDuration.textContent = '';
    return;
  }

  const result = job.result;
  if (!result || !result.Results) {
    emptyState.classList.remove('hidden');
    return;
  }

  const detections = result.Results;
  if (detections.length === 0) {
    emptyState.classList.remove('hidden');
    return;
  }

  // Group by category
  const grouped = {};
  for (const d of detections) {
    const cat = d.Technology.category;
    if (!grouped[cat]) grouped[cat] = [];
    grouped[cat].push(d);
  }

  // Sort categories within CATEGORY_ORDER (kept for backward compat in any consumer)
  const sortedCats = Object.keys(grouped).sort((a, b) => {
    const ai = CATEGORY_ORDER.indexOf(a);
    const bi = CATEGORY_ORDER.indexOf(b);
    if (ai === -1 && bi === -1) return a.localeCompare(b);
    if (ai === -1) return 1;
    if (bi === -1) return -1;
    return ai - bi;
  });

  // 1. Summary banner with key facts
  renderSummaryBanner(grouped, sortedCats);

  // 2. Marketing & Tag Managers highlight (if any)
  renderMarketingHighlight(grouped);

  // 3. Wappalyzer-style top-level group sections (skip marketing-tagmanagers already shown above)
  let html = '';
  const groupsPresent = {};
  for (const cat of Object.keys(grouped)) {
    const g = CATEGORY_GROUP[cat];
    if (!g) continue;
    if (MARKETING_CATEGORIES.has(cat)) continue; // already highlighted
    if (!groupsPresent[g]) groupsPresent[g] = [];
    groupsPresent[g].push(cat);
  }

  const sortedGroups = Object.keys(groupsPresent).sort((a, b) => {
    const ai = GROUP_ORDER.indexOf(a);
    const bi = GROUP_ORDER.indexOf(b);
    if (ai === -1 && bi === -1) return a.localeCompare(b);
    if (ai === -1) return 1;
    if (bi === -1) return -1;
    return ai - bi;
  });

  for (const groupKey of sortedGroups) {
    const meta = GROUP_META[groupKey] || { label: groupKey, accent: 'text-slate-300', dot: 'bg-slate-400' };
    const subcats = groupsPresent[groupKey].sort((a, b) => {
      const ai = CATEGORY_ORDER.indexOf(a);
      const bi = CATEGORY_ORDER.indexOf(b);
      if (ai === -1 && bi === -1) return a.localeCompare(b);
      if (ai === -1) return 1;
      if (bi === -1) return -1;
      return ai - bi;
    });

    const totalInGroup = subcats.reduce((sum, cat) => sum + grouped[cat].length, 0);

    html += `
      <section class="animate-fade-in">
        <div class="flex items-center gap-2 mb-2 sm:mb-3 px-1">
          <div class="w-1.5 h-1.5 rounded-full ${meta.dot}"></div>
          <h3 class="font-display font-semibold text-xs ${meta.accent} uppercase tracking-wider">${escapeHtml(meta.label)}</h3>
          <span class="text-[10px] font-mono text-slate-500">${totalInGroup}</span>
          <div class="flex-1 h-px bg-white/5 ml-1"></div>
        </div>
        <div class="grid grid-cols-1 xl:grid-cols-2 gap-2 sm:gap-3 items-stretch">
          ${subcats.map(cat => renderCategoryCard(cat, grouped[cat], /* compact */ true)).join('')}
        </div>
      </section>
    `;
  }
  categoriesContainer.innerHTML = html;

  // Implied
  const implied = scannerImplied(detections);
  if (implied && implied.length > 0) {
    impliedContainer.classList.remove('hidden');
    impliedList.innerHTML = implied.map(slug => {
      const icon = iconUrlFor(slug);
      let iconHtml = '';
      if (icon.type === 'img') {
        iconHtml = `<img src="${icon.url}" class="tech-icon" alt="" loading="lazy" onerror="this.outerHTML=makeFallbackLetter('${slug}','${slug}',18).outerHTML">`;
      } else {
        iconHtml = fallbackLetterHTML(slug, slug, 18);
      }
      const info = fingerprintsMap[slug];
      const website = info?.website;
      const name = info?.name || slug;
      const Tag = website ? 'a' : 'span';
      const baseCls = 'inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-sm transition-colors';
      const styleCls = website
        ? `${baseCls} bg-cyan-500/10 border border-cyan-500/30 text-cyan-300 hover:bg-cyan-500/20 hover:border-cyan-400 cursor-pointer`
        : `${baseCls} bg-cyan-500/10 border border-cyan-500/30 text-cyan-300`;
      const linkAttrs = website
        ? `href="${escapeHtml(website)}" target="_blank" rel="noopener noreferrer" title="Open ${escapeHtml(name)} website"`
        : '';
      const linkIcon = website
        ? `<svg class="w-2.5 h-2.5 opacity-70" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"/></svg>`
        : '';
      return `
        <${Tag} ${linkAttrs} class="${styleCls}">
          ${iconHtml}
          <span class="font-mono text-xs">${escapeHtml(slug)}</span>
          ${linkIcon}
        </${Tag}>
      `;
    }).join('');
  }

  // Emails found (mailto: + plain-text extraction)
  const emails = (Array.isArray(result.Emails)) ? result.Emails : [];
  if (emails.length > 0) {
    emailsContainer.classList.remove('hidden');
    emailsCount.textContent = emails.length;
    emailsList.innerHTML = emails.map(email => {
      const e = escapeHtml(email);
      return `
        <div class="group inline-flex items-center gap-1 px-2.5 py-1.5 rounded-lg bg-amber-500/10 border border-amber-500/30 hover:border-amber-400 transition-colors">
          <a href="mailto:${e}" title="Send email to ${e}" class="text-sm font-mono text-amber-300 hover:text-amber-200 cursor-pointer">${e}</a>
          <button type="button" data-email="${e}" class="copy-email-btn p-1 rounded hover:bg-amber-500/20 text-amber-300/70 hover:text-amber-200 transition-colors cursor-pointer" title="Copy to clipboard">
            <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"/></svg>
          </button>
        </div>
      `;
    }).join('');
    // Wire up copy buttons (event delegation on list)
    emailsList.querySelectorAll('.copy-email-btn').forEach(btn => {
      btn.addEventListener('click', async () => {
        const email = btn.dataset.email;
        try {
          await navigator.clipboard.writeText(email);
          const orig = btn.innerHTML;
          btn.innerHTML = '<svg class="w-3 h-3 text-green-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/></svg>';
          setTimeout(() => { btn.innerHTML = orig; }, 1200);
        } catch (e) {
          console.warn('clipboard write failed', e);
        }
      });
    });
  }
}

function scannerImplied(detections) {
  const detected = new Set(detections.map(d => d.Technology.slug));
  const implied = [];
  const seen = new Set();
  for (const d of detections) {
    if (d.Technology.implies) {
      for (const impSlug of d.Technology.implies) {
        if (!detected.has(impSlug) && !seen.has(impSlug)) {
          implied.push(impSlug);
          seen.add(impSlug);
        }
      }
    }
  }
  return implied;
}

// ===== Recent =====
function getRecent() {
  try { return JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]'); }
  catch { return []; }
}

function addRecent(url) {
  const recent = getRecent().filter(r => r.url !== url);
  recent.unshift({ url, time: Date.now() });
  localStorage.setItem(STORAGE_KEY, JSON.stringify(recent.slice(0, 10)));
  renderRecent();
}

function renderRecent() {
  const recent = getRecent();
  if (recent.length === 0) {
    recentSection.classList.add('hidden');
    return;
  }
  recentSection.classList.remove('hidden');
  recentList.innerHTML = recent.map(r => `
    <button
      data-url="${escapeHtml(r.url)}"
      class="recent-item w-full text-left px-4 py-2.5 rounded-lg bg-white/[0.02] hover:bg-white/5 border border-white/5 hover:border-white/10 transition-colors cursor-pointer flex items-center justify-between gap-3 group"
    >
      <div class="flex items-center gap-3 min-w-0 flex-1">
        <svg viewBox="0 0 24 24" class="w-3.5 h-3.5 text-slate-600 group-hover:text-slate-400 transition-colors flex-shrink-0" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="12" r="10"></circle>
          <polyline points="12 6 12 12 16 14"></polyline>
        </svg>
        <span class="font-mono text-sm text-slate-300 truncate">${escapeHtml(r.url)}</span>
      </div>
      <span class="text-xs text-slate-600 font-mono flex-shrink-0">${timeAgo(r.time)}</span>
    </button>
  `).join('');

  document.querySelectorAll('.recent-item').forEach(btn => {
    btn.addEventListener('click', () => {
      const url = btn.dataset.url;
      input.value = url;
      submitScan(url);
    });
  });
}

function timeAgo(timestamp) {
  const seconds = Math.floor((Date.now() - timestamp) / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

// ===== Auth modal handlers =====
let activeAuthTab = 'basic';

function showAuthModal() {
  authModal.classList.remove('hidden');
  authModal.classList.add('flex');
  switchAuthTab(activeAuthTab);
  prefillAuthForm();
}

function hideAuthModal() {
  authModal.classList.add('hidden');
  authModal.classList.remove('flex');
  authStatus.classList.add('hidden');
}

function switchAuthTab(tab) {
  activeAuthTab = tab;
  document.querySelectorAll('.tab-btn').forEach(b => {
    if (b.dataset.tab === tab) {
      b.classList.add('active');
      b.classList.remove('text-slate-500');
    } else {
      b.classList.remove('active');
      b.classList.add('text-slate-500');
    }
  });
  document.querySelectorAll('[data-panel]').forEach(p => {
    if (p.dataset.panel === tab) p.classList.remove('hidden');
    else p.classList.add('hidden');
  });
}

function prefillAuthForm() {
  const a = getAuth();
  if (!a) return;
  if (a.type === 'apikey') {
    apikeyInput.value = a.token;
  } else if (a.type === 'basic') {
    basicUserInput.value = a.user;
    basicPassInput.value = a.pass;
  } else if (a.type === 'jwt') {
    jwtInput.value = a.token;
  }
}

function setAuthStatus(message, kind = 'info') {
  authStatus.classList.remove('hidden');
  authStatus.className = 'text-xs font-mono rounded-lg px-3 py-2';
  const colors = {
    info: 'bg-blue-500/10 border border-blue-500/30 text-blue-300',
    success: 'bg-emerald-500/10 border border-emerald-500/30 text-emerald-300',
    error: 'bg-red-500/10 border border-red-500/30 text-red-300',
  };
  authStatus.className += ' ' + (colors[kind] || colors.info);
  authStatus.textContent = message;
}

async function verifyAuth(headers) {
  try {
    const res = await fetch(`${API_BASE}/api/auth/usage`, { headers });
    if (res.status === 200) {
      const data = await res.json();
      return { ok: true, data };
    }
    if (res.status === 401) return { ok: false, error: 'Invalid credentials' };
    if (res.status === 429) return { ok: false, error: 'Rate limit exceeded' };
    const err = await res.json().catch(() => ({}));
    return { ok: false, error: err.error || `HTTP ${res.status}` };
  } catch (e) {
    return { ok: false, error: e.message };
  }
}

authForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  authSubmitBtn.disabled = true;
  authSubmitBtn.textContent = 'Verifying…';
  let headers, auth;

  if (activeAuthTab === 'apikey') {
    const token = apikeyInput.value.trim();
    if (!token) { setAuthStatus('Enter an API key', 'error'); reset(); return; }
    headers = { 'X-API-Key': token };
    auth = { type: 'apikey', scheme: 'header', token, user: `key…${token.slice(-4)}` };
  } else if (activeAuthTab === 'basic') {
    const user = basicUserInput.value.trim();
    const pass = basicPassInput.value;
    if (!user || !pass) { setAuthStatus('Enter username and password', 'error'); reset(); return; }
    headers = { 'Authorization': 'Basic ' + btoa(`${user}:${pass}`) };
    auth = { type: 'basic', user, pass, token: btoa(`${user}:${pass}`) };
  } else if (activeAuthTab === 'jwt') {
    const token = jwtInput.value.trim();
    if (!token) { setAuthStatus('Enter a JWT', 'error'); reset(); return; }
    headers = { 'Authorization': `Bearer ${token}` };
    auth = { type: 'jwt', token, user: 'jwt-user' };
  }

  const result = await verifyAuth(headers);
  if (result.ok) {
    setAuth(auth);
    setAuthStatus(`✓ Signed in as ${auth.user}. Closing…`, 'success');
    setTimeout(hideAuthModal, 800);
  } else {
    setAuthStatus(`✗ ${result.error}`, 'error');
  }
  reset();

  function reset() {
    authSubmitBtn.disabled = false;
    authSubmitBtn.textContent = 'Verify';
  }
});

authClearBtn.addEventListener('click', () => {
  setAuth(null);
  apikeyInput.value = '';
  basicUserInput.value = '';
  basicPassInput.value = '';
  jwtInput.value = '';
  setAuthStatus('Credentials cleared', 'info');
  setTimeout(hideAuthModal, 600);
});

authBtn.addEventListener('click', showAuthModal);
closeAuthBtn.addEventListener('click', hideAuthModal);
authModal.addEventListener('click', (e) => {
  if (e.target === authModal) hideAuthModal();
});

document.querySelectorAll('.tab-btn').forEach(btn => {
  btn.addEventListener('click', () => switchAuthTab(btn.dataset.tab));
});

// Install guide modal
openInstallBtn?.addEventListener('click', () => {
  installModal.classList.remove('hidden');
  installModal.classList.add('flex');
});
openExtensionBtn?.addEventListener('click', () => {
  installModal.classList.remove('hidden');
  installModal.classList.add('flex');
});
closeInstallBtn?.addEventListener('click', () => {
  installModal.classList.add('hidden');
  installModal.classList.remove('flex');
});
installModal?.addEventListener('click', (e) => {
  if (e.target === installModal) {
    installModal.classList.add('hidden');
    installModal.classList.remove('flex');
  }
});

// Copy JSON
copyJsonBtn.addEventListener('click', async () => {
  if (!currentJobId) return;
  try {
    const res = await fetch(`${API_BASE}/api/results/${currentJobId}`, { headers: authHeaders() });
    const data = await res.json();
    await navigator.clipboard.writeText(JSON.stringify(data, null, 2));
    const original = copyJsonBtn.innerHTML;
    copyJsonBtn.innerHTML = '<svg viewBox="0 0 24 24" class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg>Copied';
    setTimeout(() => { copyJsonBtn.innerHTML = original; }, 1500);
  } catch (err) {
    console.error('Copy failed:', err);
  }
});

// Form submit
form.addEventListener('submit', (e) => {
  e.preventDefault();
  const url = input.value.trim();
  if (url) submitScan(url);
});

// Example buttons
document.querySelectorAll('.example-btn').forEach(btn => {
  btn.addEventListener('click', () => {
    const url = btn.dataset.url;
    input.value = url;
    submitScan(url);
  });
});

// ===== Health + Stats =====
async function checkHealth() {
  try {
    const res = await fetch(`${API_BASE}/api/health`);
    if (res.ok) {
      statusIndicator.innerHTML = '<div class="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse"></div><span class="text-emerald-400">ready</span>';
    } else {
      throw new Error('not ok');
    }
  } catch {
    statusIndicator.innerHTML = '<div class="w-1.5 h-1.5 rounded-full bg-red-400"></div><span class="text-red-400">offline</span>';
  }
}

async function updateQueueStats() {
  try {
    const res = await fetch(`${API_BASE}/api/stats`, { headers: authHeaders() });
    if (res.status === 401) return;
    const stats = await res.json();
    const q = stats.queue || {};
    const c = stats.cache || {};
    let statsText = `workers: 2 · ${q.completed || 0} done`;
    if (q.pending) statsText += ` · ${q.pending} pending`;
    if (c.valid !== undefined) {
      statsText += ` · cache: ${c.valid}`;
      if (c.hit_rate !== undefined && c.hits + c.misses > 0) {
        statsText += ` (${(c.hit_rate * 100).toFixed(0)}% hit)`;
      }
    }
    queueStatsEl.textContent = statsText;
  } catch {
    // ignore
  }
}

// ===== Init =====
async function loadIconManifest() {
  try {
    const [manifest, inline] = await Promise.all([
      fetch('/static/icons.json').then(r => r.json()).catch(() => ({})),
      fetch('/static/inline-icons.json').then(r => r.json()).catch(() => ({})),
    ]);
    ICON_MANIFEST = manifest;
    INLINE_ICONS = inline;
  } catch (e) {
    console.warn('Icon manifest failed to load', e);
  }
}

(async () => {
  await loadIconManifest();
  loadFingerprintsMap(); // fire-and-forget; implied links will work once loaded
  renderAuthBtn();
  checkHealth();
  renderRecent();
  updateQueueStats();
  setInterval(updateQueueStats, 10000);
  // Quota, schedules, webhook log self-hide on 404 (no-auth mode).
  refreshQuotaWidget();
  setInterval(refreshQuotaWidget, 60000);
  renderSchedules();
  setInterval(renderSchedules, 30000);
  renderWebhookLog();
  setInterval(renderWebhookLog, 30000);
  attachScheduleUI();
  input.focus();
})();

// ===== Quota widget =====
// Toggles visibility on the wrapper (`quota-container`); responsive
// variants (`quota-widget` for sm+, `quota-mobile` for < sm) handle
// which one is shown per viewport.
const quotaContainer = document.getElementById('quota-container');
const quotaWidget = document.getElementById('quota-widget');
const quotaUsed = document.getElementById('quota-used');
const quotaLimit = document.getElementById('quota-limit');
const quotaBar = document.getElementById('quota-bar');
const quotaMobileText = document.getElementById('quota-mobile-text');

async function refreshQuotaWidget() {
  try {
    const res = await fetch(`${API_BASE}/api/auth/quota`, { headers: authHeaders() });
    if (res.status === 404) {
      quotaContainer.classList.add('hidden');
      return;
    }
    if (!res.ok) return;
    const q = await res.json();
    quotaUsed.textContent = q.unlimited ? '∞' : q.used;
    quotaLimit.textContent = q.unlimited ? '∞' : q.limit;
    quotaMobileText.textContent = q.unlimited ? '∞' : `${q.used}/${q.limit}`;
    const pct = q.unlimited ? 0 : Math.min(100, (q.used / q.limit) * 100);
    quotaBar.style.width = pct + '%';
    quotaBar.classList.remove('bg-emerald-500', 'bg-amber-500', 'bg-red-500');
    if (q.unlimited) {
      quotaBar.classList.add('bg-emerald-500');
    } else if (pct >= 90) {
      quotaBar.classList.add('bg-red-500');
    } else if (pct >= 70) {
      quotaBar.classList.add('bg-amber-500');
    } else {
      quotaBar.classList.add('bg-emerald-500');
    }
    quotaContainer.classList.remove('hidden');
    const tooltip = q.unlimited
      ? `Unlimited scans (${q.tier})`
      : `${q.remaining} scans remaining this month (resets ${new Date(q.month_reset).toLocaleDateString()})`;
    quotaWidget.title = tooltip;
    if (quotaMobileText.parentElement) quotaMobileText.parentElement.title = tooltip;
  } catch {
    // ignore
  }
}

// ===== Download report buttons =====
const downloadHtmlBtn = document.getElementById('download-html-btn');
const downloadPdfBtn = document.getElementById('download-pdf-btn');

function showDownloadButtons() {
  if (downloadHtmlBtn) downloadHtmlBtn.classList.remove('hidden');
  if (downloadPdfBtn) downloadPdfBtn.classList.remove('hidden');
}
function hideDownloadButtons() {
  if (downloadHtmlBtn) downloadHtmlBtn.classList.add('hidden');
  if (downloadPdfBtn) downloadPdfBtn.classList.add('hidden');
}

if (downloadHtmlBtn) {
  downloadHtmlBtn.addEventListener('click', () => {
    if (!currentJobId) return;
    window.open(`${API_BASE}/api/report/${currentJobId}`, '_blank');
  });
}
if (downloadPdfBtn) {
  downloadPdfBtn.addEventListener('click', () => {
    if (!currentJobId) return;
    window.location.href = `${API_BASE}/api/report/${currentJobId}.pdf`;
  });
}

// ===== Schedules =====
const schedulesSection = document.getElementById('schedules-section');
const schedulesList = document.getElementById('schedules-list');
const schedulesCount = document.getElementById('schedules-count');
const schedulesEmpty = document.getElementById('schedules-empty');
const webhookLogSection = document.getElementById('webhook-log-section');
const webhookLogList = document.getElementById('webhook-log-list');

function showSchedules() {
  if (schedulesSection) schedulesSection.classList.remove('hidden');
}
function hideSchedules() {
  if (schedulesSection) schedulesSection.classList.add('hidden');
}

async function renderSchedules() {
  if (!schedulesSection) return;
  try {
    const res = await fetch(`${API_BASE}/api/schedules`, { headers: authHeaders() });
    if (res.status === 404 || res.status === 401) {
      hideSchedules();
      return;
    }
    if (!res.ok) return;
    const data = await res.json();
    const schedules = data.schedules || [];
    showSchedules();
    schedulesCount.textContent = schedules.length ? `(${schedules.length})` : '';
    if (!schedules.length) {
      schedulesList.innerHTML = '';
      schedulesEmpty.classList.remove('hidden');
      webhookLogSection.classList.add('hidden');
      return;
    }
    schedulesEmpty.classList.add('hidden');
    schedulesList.innerHTML = schedules.map(scheduleCard).join('');
    // Wire up action buttons.
    schedulesList.querySelectorAll('[data-action="toggle"]').forEach(el => {
      el.addEventListener('click', () => toggleSchedule(el.dataset.id, el.dataset.enabled === 'true' ? false : true));
    });
    schedulesList.querySelectorAll('[data-action="run"]').forEach(el => {
      el.addEventListener('click', () => runSchedule(el.dataset.id));
    });
    schedulesList.querySelectorAll('[data-action="delete"]').forEach(el => {
      el.addEventListener('click', () => deleteSchedule(el.dataset.id));
    });
  } catch (e) {
    console.warn('renderSchedules failed', e);
  }
}

function scheduleCard(s) {
  const status = s.last_status || 'idle';
  const statusBadge = {
    success: '<span class="text-emerald-400">●</span> ok',
    failed:  '<span class="text-red-400">●</span> failed',
    idle:    '<span class="text-slate-500">●</span> —',
  }[status] || '<span class="text-slate-500">●</span>';
  const nextRun = s.next_run ? new Date(s.next_run).toLocaleString() : '—';
  const lastRun = s.last_run && s.last_run !== '0001-01-01T00:00:00Z'
    ? new Date(s.last_run).toLocaleString() : '—';
  const webhook = s.webhook_url
    ? `<span class="text-slate-500">→</span> <span class="text-slate-400 truncate">${escapeHtml(s.webhook_url)}</span>${s.has_secret ? ' <span class="text-emerald-500 text-[10px]">🔒</span>' : ''}`
    : '';
  return `
    <div class="bg-bg-surface/40 border border-white/5 rounded-xl p-4 hover:border-white/10 transition-colors">
      <div class="flex items-start gap-3">
        <button data-action="toggle" data-id="${s.id}" data-enabled="${s.enabled}"
          class="mt-1 w-9 h-5 rounded-full ${s.enabled ? 'bg-accent' : 'bg-white/10'} relative transition-colors cursor-pointer flex-shrink-0"
          title="${s.enabled ? 'Disable' : 'Enable'}">
          <span class="absolute top-0.5 ${s.enabled ? 'right-0.5' : 'left-0.5'} w-4 h-4 bg-white rounded-full transition-all"></span>
        </button>
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2 mb-1">
            <div class="font-mono text-sm text-slate-200 truncate">${escapeHtml(s.url)}</div>
            <span class="text-[10px] font-mono px-1.5 py-0.5 rounded bg-white/5 text-slate-400 uppercase">${escapeHtml(s.interval)}</span>
          </div>
          <div class="flex flex-wrap gap-x-4 gap-y-1 text-xs font-mono text-slate-500">
            <span>${statusBadge}</span>
            <span>last: ${lastRun}</span>
            <span>next: ${nextRun}</span>
            <span>${s.run_count} runs</span>
            ${webhook ? `<span class="truncate max-w-xs">${webhook}</span>` : ''}
          </div>
          ${s.last_error ? `<div class="text-xs text-red-400 font-mono mt-1 truncate">${escapeHtml(s.last_error)}</div>` : ''}
        </div>
        <div class="flex items-center gap-1 flex-shrink-0">
          <button data-action="run" data-id="${s.id}" class="p-2 rounded-lg bg-white/5 hover:bg-white/10 text-slate-400 hover:text-slate-200 transition-colors cursor-pointer" title="Run now">
            <svg viewBox="0 0 24 24" class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polygon points="5 3 19 12 5 21 5 3"></polygon>
            </svg>
          </button>
          <button data-action="delete" data-id="${s.id}" class="p-2 rounded-lg bg-white/5 hover:bg-red-500/10 text-slate-400 hover:text-red-400 transition-colors cursor-pointer" title="Delete">
            <svg viewBox="0 0 24 24" class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="3 6 5 6 21 6"></polyline>
              <path d="M19 6l-2 14a2 2 0 0 1-2 2H9a2 2 0 0 1-2-2L5 6"></path>
            </svg>
          </button>
        </div>
      </div>
    </div>
  `;
}

async function toggleSchedule(id, enabled) {
  try {
    await fetch(`${API_BASE}/api/schedules/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json', ...authHeaders() },
      body: JSON.stringify({ enabled }),
    });
    renderSchedules();
  } catch (e) { console.warn(e); }
}
async function runSchedule(id) {
  try {
    await fetch(`${API_BASE}/api/schedules/${id}/run`, {
      method: 'POST',
      headers: authHeaders(),
    });
    renderSchedules();
  } catch (e) { console.warn(e); }
}
async function deleteSchedule(id) {
  if (!confirm('Delete this schedule?')) return;
  try {
    await fetch(`${API_BASE}/api/schedules/${id}`, {
      method: 'DELETE',
      headers: authHeaders(),
    });
    renderSchedules();
  } catch (e) { console.warn(e); }
}

// ===== Webhook log =====
async function renderWebhookLog() {
  if (!webhookLogSection) return;
  try {
    const res = await fetch(`${API_BASE}/api/webhooks/log`, { headers: authHeaders() });
    if (!res.ok) { webhookLogSection.classList.add('hidden'); return; }
    const data = await res.json();
    const deliveries = data.deliveries || [];
    if (!deliveries.length) { webhookLogSection.classList.add('hidden'); return; }
    webhookLogSection.classList.remove('hidden');
    webhookLogList.innerHTML = deliveries.slice(0, 10).map(d => {
      const ts = new Date(d.timestamp).toLocaleString();
      const code = d.http_code ? ` · HTTP ${d.http_code}` : '';
      const dur = d.duration_ms ? ` · ${d.duration_ms}ms` : '';
      const dot = d.status === 'success' ? '<span class="text-emerald-400">●</span>' : '<span class="text-red-400">●</span>';
      return `
        <div class="flex items-center gap-2 text-xs font-mono text-slate-500 py-1">
          ${dot}
          <span class="text-slate-400">${escapeHtml(d.url)}</span>
          <span>${code}${dur}</span>
          <span class="ml-auto">${ts}</span>
        </div>
      `;
    }).join('');
  } catch (e) { console.warn(e); }
}

// ===== New schedule modal =====
const scheduleModal = document.getElementById('schedule-modal');
const scheduleForm = document.getElementById('schedule-form');
const scheduleUrlInput = document.getElementById('schedule-url');
const scheduleIntervalSelect = document.getElementById('schedule-interval');
const scheduleWebhookUrlInput = document.getElementById('schedule-webhook-url');
const scheduleWebhookSecretInput = document.getElementById('schedule-webhook-secret');
const scheduleError = document.getElementById('schedule-error');

function attachScheduleUI() {
  const open = () => {
    scheduleModal.classList.remove('hidden');
    scheduleModal.classList.add('flex');
    scheduleUrlInput.focus();
  };
  const close = () => {
    scheduleModal.classList.add('hidden');
    scheduleModal.classList.remove('flex');
    scheduleForm.reset();
    scheduleError.classList.add('hidden');
  };
  document.getElementById('new-schedule-btn')?.addEventListener('click', open);
  document.getElementById('close-schedule-btn')?.addEventListener('click', close);
  document.getElementById('cancel-schedule-btn')?.addEventListener('click', close);
  scheduleModal.addEventListener('click', e => { if (e.target === scheduleModal) close(); });
  scheduleForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    scheduleError.classList.add('hidden');
    const body = {
      url: scheduleUrlInput.value.trim(),
      interval: scheduleIntervalSelect.value,
    };
    if (scheduleWebhookUrlInput.value.trim()) body.webhook_url = scheduleWebhookUrlInput.value.trim();
    if (scheduleWebhookSecretInput.value.trim()) body.webhook_secret = scheduleWebhookSecretInput.value.trim();
    try {
      const res = await fetch(`${API_BASE}/api/schedules`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify(body),
      });
      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: 'request failed' }));
        scheduleError.textContent = err.error || 'Failed to create schedule';
        scheduleError.classList.remove('hidden');
        return;
      }
      close();
      renderSchedules();
    } catch (err) {
      scheduleError.textContent = 'Network error: ' + err.message;
      scheduleError.classList.remove('hidden');
    }
  });
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  }[c]));
}