# Backlog

Everything the shipped tiers deliberately left undone, with the spec line that
deferred it and the code that shows the gap today. This file is the roadmap:
tiers are cut from it, and an item leaves only when a tier ships it.

Ordering below is the recommended order, not a promise.

---

## 1. Release metadata: version stamping + CHANGELOG (tier 4b)

**Deferred by:** `specs/2026-07-21-tier4a-release-gate-design.md:188-193` - "Code
signing and notarization, an auto-update channel, version stamping via ldflags,
and a CHANGELOG. Each is its own piece of work."

**Gap today:** no `-ldflags` anywhere in the tree, so a built binary does not
know its own version and the app has no way to report one. `git tag` is empty -
no release has been cut yet, so the first tag is also the first chance to get
this right. No `CHANGELOG.md`.

**Why it is first:** it is the cheapest item on this list and it bolts straight
onto the release pipeline tier 4a just built. Without it a bug report cannot
name a build.

Out of this item on purpose: code signing / notarization (needs a paid Apple
Developer certificate and a Windows cert), and an auto-update channel (the
largest single piece of work on this page).

## 2. Git conflict recovery (tier 4c)

**Deferred by:** `specs/2026-07-18-tier3d-local-data-integrity-design.md:158-160`
- "EXPLICITLY NOT IN SCOPE: `MergeContinue`/`MergeAbort`/`RebaseContinue`/
`RebaseAbort` bindings, per-file ours/theirs resolution, the `CommitBox` conflict
UI. That L is the anchor of a later git-recovery tier."

**Gap today:** `internal/git/ops.go:379` flags unmerged paths as `Conflict` and
`CommitStaged`/`CommitAll` refuse to commit while one exists, and tier 3d stopped
fleet from aborting a merge it did not start. What is missing is the other half:
there is no binding that can *finish* a conflicted merge or rebase. Once a user
lands in a conflict, fleet can only describe it - the resolution happens in a
terminal.

**Why it is second:** it is the last workflow in the app that dead-ends the user
into leaving it, and tier 3d already named this as the anchor.

## 3. Intel sync: briefings and repo chat off `localStorage`

**Deferred by:** `specs/2026-07-09-fleet-backend-spine-design.md:278-279` - "Intel
sync (briefings, per-repo chats) - slice 0.5, after relocating intel from
`localStorage` into a Go-backed store keyed by a stable repo identity."

**Gap today:** `frontend/src/lib/agentSession.ts:47,54` and
`frontend/src/lib/RepoChat.svelte:78,88` read and write chat turns straight to
`localStorage`. That data is outside the Go store, so it is not synced, not in
the JSON export, and gone on a new machine.

**Blocked on:** a stable repo identity key. A filesystem path is not one - it
differs per machine, which is exactly what makes the current key unsyncable.

## 4. Backups and export are one-way

**Deferred by:** `specs/2026-07-18-tier3d-local-data-integrity-design.md:126-127`
("Restoring a backup into the store is deliberately out of scope: it needs a
re-push story") and `:176-180`.

**Gap today:** `app.go:333 ConflictBackups` lists what sync overwrote, and the
data export writes JSON out. Neither has an inbound path - no restore, no
import. A user who spots the record sync clobbered still cannot get it back.

**Blocked on:** deciding what a restore does to the sync version vector, i.e.
whether a restored record re-pushes as a new version or is a purely local
resurrection.

---

## Smaller deferrals, recorded so they are not rediscovered

| Item | Deferred by |
| --- | --- |
| Interactive hunk staging, cherry-pick, interactive rebase, reflog | `specs/2026-07-16-tier3a-git-depth-design.md:84-87` |
| `--force` branch delete from the UI | same |
| Selective per-project sync; import of an export; undo for account deletion | `specs/2026-07-16-tier2c-sync-account-design.md:74-77` |
| Fuzzy file matching/ranking; regex and case options in search | `specs/2026-07-11-fleet-search-enhancements-design.md:63-68` |
| Per-editor launch-argument templates (`code -g file:line`) | `specs/2026-07-12-fleet-editor-picker-design.md:53-58` |
| Fleet MCP server exposing cross-repo/PM tools to the agent | `specs/2026-07-10-fleet-intel-agent-design.md:209-214` |
| Multi-repo briefing correlation; multi-root agent reads (v1 is `Roots[0]`) | same, and `specs/2026-07-13-fleet-cross-repo-agent-design.md:59-64` |
| True secret containment for agent Grep/Glob (needs an OS sandbox) | `specs/2026-07-11-fleet-agent-hardening-actions-design.md:124` and `specs/2026-07-13-grep-secret-guard-design.md:70-72` |
| Access-token revocation/denylist; per-device session listing | `specs/2026-07-13-refresh-token-family-design.md:82-87` |
| Env-tunable server timeouts and GC interval; DB pool sizing | `specs/2026-07-13-server-lifecycle-resilience-design.md:77-82`, `specs/2026-07-14-refresh-token-gc-design.md:62-66` |
| Distributed tracing / OTel spans; per-endpoint SLOs | `specs/2026-07-14-server-metrics-design.md:75-80` |
| `auto_fetch_minutes` timer, sort persistence, bounded worker pool | `specs/2026-07-01-fleet-gui-design.md:80-84` |
