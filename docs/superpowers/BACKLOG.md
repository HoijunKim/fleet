# Backlog

Everything the shipped tiers deliberately left undone. This file is the roadmap:
an item leaves the "Remaining" list only when a tier ships it.

---

## Shipped

| Item | Tier |
| --- | --- |
| Release metadata: version stamping (ldflags) + CHANGELOG | 4b |
| Git conflict recovery (Merge/Rebase continue/abort, per-file resolve) | 4c |
| Intel (brief + chat) relocated into a Go store, keyed by stable identity | 4d |
| Intel sync across devices (last-write-wins) | 4e |
| Restore a conflict backup (re-stamped so it re-pushes and wins) | 4f |
| Import an export (upsert, never deleting local-only records) | 4g |
| GUI click-wiring interaction tests (happy-dom harness) | 4h |
| `--force` branch delete from the UI | 4i |
| Case-insensitive content search + match-case toggle | 4j |
| Project-table sort persistence | 4k |
| Cherry-pick a commit from another branch (conflicts via the 4c panel) | 4l |
| Reflog recovery (move the branch back to a previous HEAD, refused when dirty) | 4m |
| PolyForm Noncommercial license + in-app help overlay | 4n |
| Fuzzy file-name search with best-first ranking | 4o |
| Content search modes: fixed-string default + regex + whole-word | 4p |
| Open editor at a line (`OpenEditorAt`) | already shipped pre-4 |

---

## Remaining

### Large - each its own tier, needs a design pass

| Item | Deferred by | Notes |
| --- | --- | --- |
| Interactive hunk staging | `specs/2026-07-16-tier3a-git-depth-design.md:84-87` | Biggest git item; a partial-stage UI over `git add -p`. |
| Interactive rebase | same | Cherry-pick (4l) and reflog (4m) shipped; interactive rebase (reorder/squash/edit) is the remaining, hardest git item. |
| Selective per-project sync; undo for account deletion | `specs/2026-07-16-tier2c-sync-account-design.md:74-77` | Sync-policy design decisions. |
| Fleet MCP server exposing cross-repo/PM tools to the agent | `specs/2026-07-10-fleet-intel-agent-design.md:209-214` | Largest single item; a whole subsystem. |
| Multi-repo briefing correlation; multi-root agent reads (v1 is `Roots[0]`) | same, and `specs/2026-07-13-fleet-cross-repo-agent-design.md:59-64` | |
| Access-token revocation/denylist; per-device session listing | `specs/2026-07-13-refresh-token-family-design.md:82-87` | Security-sensitive; its own review. |

### Ops - low user value, "if needed"

| Item | Deferred by |
| --- | --- |
| Env-tunable server timeouts and GC interval; DB pool sizing | `specs/2026-07-13-server-lifecycle-resilience-design.md:77-82`, `specs/2026-07-14-refresh-token-gc-design.md:62-66` |
| `auto_fetch_minutes` timer, bounded scan worker pool | `specs/2026-07-01-fleet-gui-design.md:80-84` |

### Needs external infrastructure - not buildable in this environment

| Item | Deferred by | Blocker |
| --- | --- | --- |
| True secret containment for agent Grep/Glob | `specs/2026-07-11-fleet-agent-hardening-actions-design.md:124`, `specs/2026-07-13-grep-secret-guard-design.md:70-72` | Needs an OS sandbox. |
| Distributed tracing / OTel spans; per-endpoint SLOs | `specs/2026-07-14-server-metrics-design.md:75-80` | Needs a collector. |
| Code signing / notarization; auto-update channel | `specs/2026-07-21-tier4a-release-gate-design.md:188-193` | Needs paid signing certs + an update host. |
