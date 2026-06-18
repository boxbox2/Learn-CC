---
name: commit
description: Prepare and create a focused git commit.
allowed_tools: [Bash, Read, Grep]
mode: inline
---

# Commit SOP

Inspect the current git status and relevant diffs before proposing a commit.
Summarize the changed intent, check for unrelated files, and only commit when the working tree scope is coherent.
Use `$ARGUMENTS` as the user's commit intent when provided.

