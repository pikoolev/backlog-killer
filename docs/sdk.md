# Embeddable SDK

`BacklogKiller.init(options)` is idempotent and returns `open`, `close`, `setMode`, `refresh`, `focusTicket`, and `destroy`. `destroy` removes the widget, listeners, observers, pending requests, and restores History methods.

## Host behavior

View mode never intercepts host actions. Annotate mode ignores links, controls, contenteditable nodes, and SDK nodes on left click. Explicit right click captures an interactive element only without modifier keys. Escape cancels selection.

Anchors prefer `data-backlog-id`, unique IDs, semantic attributes, then structural selectors. Open shadow roots use selector segments. Resolution validates a fingerprint and falls back to scored text/structure; ambiguous or missing targets render no marker and report `anchor unresolved`. Closed shadow roots resolve only to their host. Cross-origin iframe DOM is unavailable.

Page scope defaults to `location.pathname`; query strings and hashes are rejected. Deep links use fragments and never carry tokens.

## Security and privacy

Allow exact origins only. CSP must permit the SDK script, API `connect-src`, blob/data images when screenshots are enabled, and the OIDC popup. Password fields and `[data-backlog-private]` content are masked before capture. Screenshot failure never blocks ticket creation. The first release does not collect console logs, network traffic, session replay, or form values.
