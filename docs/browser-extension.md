# Backlog Killer browser extension

The Chromium Manifest V3 extension lives in `apps/extension` and opens as a browser side panel.

## Authentication

The extension uses Authorization Code + PKCE through `chrome.identity.launchWebAuthFlow`. Credentials are entered only on `auth.webmaker.pw`; the extension never receives or persists a password. Access and refresh tokens use `chrome.storage.session`, which is cleared when the browser session ends or the user signs out.

Release 0.2.3 uses a new session schema and requires an explicit login after installation or update. Login always requests password reauthentication rather than silently adopting an existing Keycloak SSO session. Logout clears local authentication and page notes even without an active website, cancels pending authenticated requests, and best-effort revokes the refresh token. Late login or refresh responses cannot restore a cleared session. Already-issued access JWTs retain their normal server-side expiry; this is not global immediate JWT revocation.

## Install production build

1. Download `https://sdk.webmaker.pw/backlog-killer-extension.zip` and extract it.
2. Open `chrome://extensions` or the equivalent Chromium extensions page.
3. Enable developer mode.
4. Choose **Load unpacked** and select the extracted directory.
5. Pin Backlog Killer and click its toolbar icon to open the side panel.

The checked-in manifest key gives unpacked builds the stable extension ID `nkkilejiaedcndfekaibdagbnodglnoe`, matching the registered Keycloak redirect URI.

For an existing unpacked installation, replace the extracted files with the new archive and press **Reload** on its card in `chrome://extensions`. Close the old side panel, reload the client page so its content script is replaced, and open the extension again. Downloading an archive alone does not update an unpacked extension.

The production client is **КонцертCRM**, project key `concert-crm`, with the exact origin `https://concert-crm.ru`. Paths such as `/gng/users` belong to the same origin; `http://concert-crm.ru`, subdomains, and other domains do not. The user needs membership in this project as well as a successful login.

## Behavior

- The active tab in the side panel's own browser window is matched against exact project origins configured in the dashboard. Switching to a browser-internal tab shows no active site instead of retaining another page's project.
- Statistics are collapsed by default; the compact summary expands to show every workflow status.
- **Поставить стикер** включает отдельный слой поверх страницы. Затем поставь черновой стикер одним левым кликом; `Esc` отменяет. Опиши задачу в панели и нажми **Создать тикет**.
- Если вкладка была открыта до обновления, content script подключается автоматически при выборе стикера. Повторное нажатие **Reload** после обновления не требуется.
- Стикер запоминает точный URL экрана, текущий внутренний маршрут/выбранную вкладку, элемент и точку клика. Привязанная заметка двигается вместе со страницей и появляется снова только на сохранённом экране. Содержимое форм не попадает в anchor.
- The isolated overlay records the full page URL, visible internal route/tab, element and click position. Sticky notes belong only to that exact screen, follow page scroll and resize, and reappear when returning to it. Text/form controls are not copied into anchor metadata.
- Active tickets for the current URL path appear on the page as yellow sticky notes anchored to their selected elements. Creating a ticket refreshes the canvas immediately.
- The current-page sidebar list is deliberately concise. Full editing, review, attachments, and agent workflow remain in the dashboard and MCP API.
