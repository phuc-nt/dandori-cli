# Intent Preservation (G8)

Captures **why** an agent ran, not just **what** it did. After every `dandori run` completes, the session JSONL is parsed and the agent's original goal, reasoning snippets, and design decisions are stored as Layer-4 semantic events. This cuts root-cause analysis (RCA) from "read the full transcript" to "read the incident report".

---

## 1. What It Captures

| Event type | Layer | Emitted | Payload |
|---|---|---|---|
| `intent.extracted` | 4 | Once per run | `first_user_msg`, `summary`, `spec_links` |
| `decision.point` | 4 | 0–5 per run | `chosen`, `rejected[]`, `rationale` |
| `agent.reasoning` | 4 | 0–10 per run | `index`, `source` (`thinking`/`narrative`), `text` |

### Sample payloads

**intent.extracted**
```json
{
  "first_user_msg": "Implement auth token refresh for CLITEST-12",
  "summary": "Added sliding-window refresh with 15-min TTL. Tests pass.",
  "spec_links": {
    "jira_key": "CLITEST-12",
    "confluence_urls": ["https://acme.atlassian.net/wiki/spaces/PROJ/pages/42"],
    "source_paths": ["README.md", "CLAUDE.md"]
  }
}
```

**decision.point**
```json
{
  "chosen": "sliding window",
  "rejected": ["fixed expiry"],
  "rationale": "matches existing refresh_token behavior in auth.go",
  "ts_offset_sec": 0
}
```

**agent.reasoning**
```json
{
  "index": 0,
  "source": "thinking",
  "text": "Considering TTL options. Sliding window fits the current pattern..."
}
```

---

## 2. How It Works

```
dandori run -- claude ...
     │
     ▼
[wrapper: fork + exec]
     │
     ├── session writes to ~/.dandori/<run_id>.jsonl
     │
     ▼
[run ends: exit code captured]
     │
     ▼
runIntentExtraction()          (wrapper/wrapper.go)
     │
     ├── intent.Extract(jsonl, runID, cwd, jiraKey)
     │     ├── Walk() — stream-parse JSONL lines
     │     ├── firstTextFromUser() → first_user_msg (2 KB cap)
     │     ├── lastTextSummary() → summary (2 KB cap)
     │     ├── appendReasoningBlocks() → agent.reasoning (10 cap, 1 KB each)
     │     ├── ExtractDecisions() → decision.point (5 cap, regex heuristics)
     │     └── ExtractSpecLinks() → spec_links (scan cwd files, extract URLs)
     │
     └── event.Recorder.RecordEvent() × N
           └── INSERT INTO events (layer=4, …)
```

Extraction is **post-run and passive** — it does not inject prompts, slow the agent, or interact with Claude. A parse error in any step is logged at Warn and the run result is unaffected.

---

## 3. Heuristic Limitations

| Limitation | Detail |
|---|---|
| **English-only** | Decision patterns (`I'll go with X because Y`, `using X over Y`, etc.) are English regex. Non-English reasoning blocks produce zero `decision.point` events. |
| **False positive cap** | Capped at 5 decisions/run. Beyond 5 the heuristic signal-to-noise ratio degrades (common words like "using" fire on non-decision sentences). |
| **Advisory framing** | Every decision in the incident report is tagged `(heuristic)`. Do not rely on them for blame attribution or compliance audits. |
| **No agent cooperation** | v1 is pure passive parsing. The agent is not prompted to mark decisions. Decisions inside multi-line reasoning blocks may be missed if the phrase spans multiple content parts. |
| **No cross-run deduplication** | The same conceptual decision recorded in two sequential runs of the same task will appear twice in `incident-report --task`. |

---

## 4. Where It Surfaces

### 4a. Jira completion comment

After `dandori jira-sync`, the run completion comment gains two sections when intent events exist:

```
✅ Agent Run Completed

*Agent:* claude-code
*Duration:* 142s
…

h3. Intent
{quote}Implement auth token refresh for CLITEST-12{quote}
*Summary:* Added sliding-window refresh with 15-min TTL.
*Specs:* CLITEST-12 · [Confluence|https://acme.atlassian.net/wiki/…]

h3. Key Decisions
1. *Chose:* sliding window
   *Over:* fixed expiry
   _Reason: matches existing refresh_token behavior_ _(heuristic)_
```

When no `intent.extracted` event exists for the run (legacy run or gate disabled) the comment is identical to the pre-G8 format — fully backwards-compatible.

### 4b. Incident report command

```bash
# Single run
dandori incident-report --run <run-id>

# All runs for a Jira task (aggregated)
dandori incident-report --task CLITEST-12
```

The report includes:
- `## Intent` — first user message, agent summary, spec sources
- `## Key Decisions` — heuristic-tagged numbered list
- `## Reasoning Trace` — top reasoning blocks (plain text)
- `## Diff Stats`, `## Tool / Skill Usage`, `## Quality` — existing sections

See `docs/02-user-guide.md` for the full command reference.

---

## 5. Disabling

Set the environment variable before starting a run:

```bash
DANDORI_INTENT_DISABLED=1 dandori run -- claude ...
```

When set to any non-empty value, `intent.Extract` returns immediately with an empty result and no Layer-4 events are written. The Jira comment and incident report fall back to pre-G8 format automatically.

This is a per-run gate. There is no config-file toggle in v1; use a shell alias or wrapper script to apply it permanently.

---

## 6. Privacy

| Concern | How G8 handles it |
|---|---|
| **API keys / tokens** | `redactSecrets()` strips `sk-*`, Bearer tokens, GitHub PATs, AWS AKIA keys, and generic `api_key=` / `password=` assignments before storing. Replacement literal: `<redacted>`. |
| **Size caps** | `first_user_msg` and `summary` capped at 2 KB. Each reasoning block capped at 1 KB. Decision fields capped at 200 chars each. |
| **No file contents** | `spec_links.source_paths` stores only the file path, not the file body. Confluence URLs are extracted from file contents but the contents themselves are not stored. |
| **No network calls** | Extraction runs entirely locally (SQLite + filesystem). No data leaves the workstation during extraction. |

---

## 7. Schema Reference

All three event types use `layer = 4` in the `events` table:

```sql
SELECT event_type, data
FROM events
WHERE run_id = '<id>'
  AND layer = 4
ORDER BY id ASC;
```

Type constants in `internal/model/event.go`:

```go
EventTypeIntentExtracted = "intent.extracted"
EventTypeAgentReasoning  = "agent.reasoning"
EventTypeDecisionPoint   = "decision.point"
```

---

## 8. Roadmap (v2)

- **Agent cooperation**: prompt the agent to emit structured `[DECISION: chose X over Y because Z]` markers during the run, eliminating heuristic regex.
- **Deep section linkage**: link each decision back to the specific Confluence section or Jira acceptance criterion it addresses.
- **Dashboard integration**: `decision.point` counts and decision-topic clustering surfaced in the monitoring dashboard.
- **Non-English support**: language-neutral decision markers via agent cooperation (see above).

---

## See Also

- [Agent Attribution (G7)](02-agent-attribution.md) — line-level blame + intervention classifier
- [DORA + Rework Rate Export (G6)](01-metric-export.md) — 5 engineering metrics, 3 wire formats
- [User Guide](../02-user-guide.md) — full command reference including `incident-report`
