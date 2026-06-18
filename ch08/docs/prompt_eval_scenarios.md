# Prompt Evaluation Scenarios

Use these scenarios for manual comparison before and after the structured system prompt changes. Run each scenario in a fresh session when possible and compare tool choices, mode adherence, verification behavior, and cache usage display.

## Read-Only Planning

Input:

```text
/plan Add validation for invalid config files.
```

Observe:

- The model may use Read, Glob, or Grep.
- The model must not use Write, Edit, or Bash.
- The final answer should be a plan, not an implementation claim.

Expected behavior:

The model inspects the project with read-only tools and returns a concrete plan.

## Read Before Edit

Input:

```text
Update the config loader error message to include the file path.
```

Observe:

- The model should locate relevant files with Glob or Grep.
- The model should Read the target file before Edit.
- The model should keep the change focused.

Expected behavior:

The model reads existing code before editing and does not guess exact file contents.

## Tool-First Search

Input:

```text
Find where tool results are truncated and explain the flow.
```

Observe:

- The model should use Grep or Glob instead of answering from memory.
- The model should Read the relevant files after locating them.
- The final answer should cite the actual flow it observed.

Expected behavior:

The model uses search tools to ground the explanation in the current codebase.

## Verify Before Reporting

Input:

```text
Make the smallest safe change needed to include cached tokens in the status bar.
```

Observe:

- The model should edit only relevant files.
- The model should run targeted tests after the edit.
- The final answer should report the verification command and result.

Expected behavior:

The model verifies with tests or clearly explains why verification could not run.

## Dynamic Environment Change

Input:

```text
Explain which directory you are working in, then inspect the prompt builder.
```

Observe:

- The working directory should come from the environment system note.
- A later run from a different directory should change only the dynamic note.
- The stable system prompt should remain identical.

Expected behavior:

The model uses the current request environment without treating it as durable user history.

## Cache Observation

Input:

```text
Summarize the prompt assembly flow.
```

Repeat with a similar request:

```text
Summarize the system prompt assembly flow again.
```

Observe:

- OpenAI usage should still show prompt, completion, and total tokens.
- If the API returns cached tokens, the status bar should include `cached=N`.
- A missing cached token field should not break streaming or display.

Expected behavior:

The second similar request can show a cache hit when the provider returns cached token details.
