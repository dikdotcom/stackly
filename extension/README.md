# Stackly — Chrome Extension

Detect the tech stack of any website with one click.

## Install (Development)

1. Open Chrome and go to `chrome://extensions/`
2. Enable **Developer mode** (top right)
3. Click **Load unpacked**
4. Select this `extension/` directory

## Configure

1. Click the Stackly icon in your browser toolbar
2. Set your API endpoint (default: `http://localhost:8890`)
3. The endpoint is saved automatically via `chrome.storage.sync`

## Use

1. Navigate to any website
2. Click the Stackly icon
3. Click **Detect Stack**
4. View detected technologies grouped by category

## Files

| File | Purpose |
|------|---------|
| `manifest.json` | Manifest V3 config |
| `popup.html` | Extension popup UI |
| `popup.js` | Popup logic (API calls, render) |
| `background.js` | Service worker (API health check) |
| `icons/` | Extension icons (16/48/128 px) |

## API Requirements

The extension requires a Stackly API server to be running and accessible at the configured endpoint. The server must have CORS enabled (default).

## Permissions

| Permission | Why |
|------------|-----|
| `activeTab` | Read URL of current tab when popup opens |
| `storage` | Save API endpoint preference |
| `<all_urls>` | (declared for future content script features) |