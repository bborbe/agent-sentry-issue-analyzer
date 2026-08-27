Your final response MUST be a single JSON object with this schema:

```json
{
  "status": "done" | "failed" | "needs_input",
  "message": "human-readable summary of what happened",
  "files": ["list", "of", "files", "created-or-modified"]
}
```

Field rules:
- `status`: required — `done` (success), `failed` (error), or `needs_input` (blocked on missing info)
- `message`: required — concise summary, one or two sentences
- `files`: optional — list of file paths created or modified during execution

Output the JSON inside a fenced code block (```json ... ```). No prose before or after the fence. The fence renders the JSON readably in Obsidian and other markdown viewers; downstream consumers strip the fence before parsing.
