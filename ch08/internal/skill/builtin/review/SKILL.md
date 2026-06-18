---
name: review
description: Review current code changes for bugs and regressions.
allowed_tools: [Bash, Read, Grep]
mode: fork
fork_context: recent
---

# Review SOP

Review the current code changes. Prioritize bugs, behavioral regressions, race conditions, missing tests, and operational risks.
Lead with findings ordered by severity and include concrete file or behavior references when available.
Use `$ARGUMENTS` as extra review focus when provided.

