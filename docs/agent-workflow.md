# Coding agent workflow

1. Authenticate with a project-bound PAT or approved OIDC token. Never place bearer tokens in URLs.
2. Call `backlog_list_tickets`; process `rework` before `in_progress`, then priority descending, deadline ascending, creation ascending.
3. Read full candidate tickets with `backlog_get_ticket`, including location, specification, prior feedback, events, work log, and attachment metadata.
4. Inspect every relevant attachment with `backlog_get_attachment`. It returns the authorized file metadata and base64 content; decode it locally and use the media type to select OCR, vision, PDF, or text processing.
5. Select one coherent batch and claim all IDs atomically with `backlog_claim_batch`. Any unavailable or foreign ID rejects the whole claim.
6. Modify the external product repository and run the real changed path. Tests alone are not completion evidence.
7. If blocked, call `backlog_add_comment` with the concrete prerequisite. Do not submit unfinished work.
8. For each completed ticket call `backlog_submit_for_review(ticketId, agentComment, workLog, version)`. `workLog` records files/symbols, design, migrations, checks, commit/deploy, and failed approaches.
9. Only a human project administrator accepts `review` to `done` or returns it to `rework` with a required comment.

MCP tools are exactly: `backlog_list_tickets`, `backlog_get_ticket`, `backlog_get_attachment`, `backlog_claim_batch`, `backlog_add_comment`, and `backlog_submit_for_review`. Workflow resource: `backlog://project/{projectId}/workflow`.
