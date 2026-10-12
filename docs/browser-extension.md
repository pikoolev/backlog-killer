# Backlog Killer browser extension

The Chromium Manifest V3 extension lives in `apps/extension` and opens as a browser side panel.

## Authentication

The extension uses Authorization Code + PKCE through `chrome.identity.launchWebAuthFlow`. Credentials are entered only on `auth.webmaker.pw`; the extension never receives or persists a password. Access and refresh tokens use `chrome.storage.session`, which is cleared when the browser session ends or the user signs out.

## Install production build

1. Download `https://sdk.webmaker.pw/backlog-killer-extension.zip` and extract it.
2. Open `chrome://extensions` or the equivalent Chromium extensions page.
3. Enable developer mode.
4. Choose **Load unpacked** and select the extracted directory.
5. Pin Backlog Killer and click its toolbar icon to open the side panel.

The checked-in manifest key gives unpacked builds the stable extension ID `nkkilejiaedcndfekaibdagbnodglnoe`, matching the registered Keycloak redirect URI.

## Behavior

- The active tab origin is matched against exact project origins configured in the dashboard.
- Statistics are collapsed by default; the compact summary expands to show every workflow status.
- **Добавить тикет** starts element selection in the active page. Press the left and right mouse buttons together on the target block, enter the task, choose priority, and submit.
- Active tickets for the current URL path appear on the page as yellow sticky notes anchored to their selected elements. Creating a ticket refreshes the canvas immediately.
- The current-page sidebar list is deliberately concise. Full editing, review, attachments, and agent workflow remain in the dashboard and MCP API.
