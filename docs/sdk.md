# Embeddable SDK

`BacklogKiller.init(options)` is idempotent and returns `open`, `close`, `setMode`, `refresh`, `focusTicket`, and `destroy`. `destroy` removes the widget, listeners, observers, pending requests, and restores History methods.

## Host behavior

View mode never intercepts host actions. Annotate mode selects an element only through an explicit unmodified right click; normal left-click navigation and controls remain untouched. Escape cancels selection.

Anchors prefer `data-backlog-id`, unique IDs, semantic attributes, then structural selectors. Open shadow roots use selector segments. Resolution validates a fingerprint and falls back to scored text/structure; ambiguous or missing targets render no marker and report `anchor unresolved`. Closed shadow roots resolve only to their host. Cross-origin iframe DOM is unavailable.

Page scope defaults to `location.pathname`; query strings and hashes are rejected. Deep links use the `backlog-ticket` query parameter and never carry tokens.

## Security and privacy

Allow exact origins only. CSP must permit the SDK script, API `connect-src`, blob/data images when screenshots are enabled, and the OIDC popup. Screenshots are cropped to the selected element; form controls and `[data-backlog-private]` content inside it are masked before capture. Screenshot failure never blocks ticket creation. The first release does not collect console logs, network traffic, session replay, or form values.

## Persistent visual backlog

After authentication the SDK follows cursor pagination for all operational and review tickets on the current `urlPath`. It renders only resolvable anchors, refreshes positions after scroll, resize, relevant DOM mutations, and SPA navigation, and reloads page-scoped tickets after navigation. Done and deleted tickets are excluded. A dashboard link with `?backlog-ticket=<uuid>` opens the detail panel and focuses the matching marker; an unresolved anchor opens detail with an explicit warning instead of placing a false marker.

Browser tokens remain memory-only and expire after five minutes. Reloading the host page therefore requires sign-in again; no bearer token is written to URL, cookies, localStorage, or sessionStorage.
