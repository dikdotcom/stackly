// Stackly — Background Service Worker
// Handles extension installation and API availability check

const DEFAULT_API = 'http://localhost:8890';

chrome.runtime.onInstalled.addListener(() => {
  console.log('[Stackly] Extension installed');
  checkApiHealth();
});

// Periodic health check
chrome.runtime.onStartup.addListener(() => {
  checkApiHealth();
});

// Check API health and notify
async function checkApiHealth() {
  const { stackly_api_url } = await chrome.storage.sync.get(['stackly_api_url']);
  const apiUrl = stackly_api_url || DEFAULT_API;

  try {
    const res = await fetch(`${apiUrl}/api/health`);
    if (res.ok) {
      console.log('[Stackly] API connected:', apiUrl);
      chrome.action.setBadgeText({ text: '' });
    } else {
      chrome.action.setBadgeText({ text: '!' });
      chrome.action.setBadgeBackgroundColor({ color: '#F87171' });
    }
  } catch (err) {
    chrome.action.setBadgeText({ text: '!' });
    chrome.action.setBadgeBackgroundColor({ color: '#F87171' });
    console.warn('[Stackly] API not reachable:', apiUrl);
  }
}

// Listen for messages from popup
chrome.runtime.onMessage.addListener((request, sender, sendResponse) => {
  if (request.action === 'checkHealth') {
    checkApiHealth().then(() => sendResponse({ ok: true }));
    return true; // async response
  }
});