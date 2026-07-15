# Tier 2c - Sync & Account Design

**Goal:** Make the sync/account surface complete and service-grade: surface
projects that synced from another device (and let the user clone them), export
local data, and delete the account.

## Sub-slice 2c-1: Synced-but-uncloned projects + Clone

**Background.** The sync engine stores a code project's PM record under a
portable `doc_id`: `git:<normalized-remote>` when the repo had a remote, else
`local:<hash>`. A record pulled from another device whose repo is not present
locally is a *detached* record - its store id keeps the `git:`/`local:` prefix
(a locally-present code project is keyed by its filesystem path instead).
`ListProjects` shows only discovered code repos and manual records, so detached
records are currently invisible.

- `App.SyncedUncloned() []UnclonedView` - snapshot the store; for each record
  whose id starts with `git:` or `local:`, emit
  `{ID, Name, Remote, TaskCount, CanClone}`. `Remote` is the https URL rebuilt
  from a `git:` id (`git:github.com/o/r` -> `https://github.com/o/r`);
  `CanClone` is true only for `git:` ids (a `local:` record has no remote).
- `App.CloneUncloned(id, destRoot string) string` - rebuild the https URL from
  the `git:` id, choose `destRoot` (default: first configured Root) +
  `/<repo-base>`, refuse if that path exists, run `git clone <url> <dest>`.
  Return "" on success (a later scan discovers the clone and the sync engine
  reconciles the detached record to it). Errors: not a `git:` id, dest exists,
  clone failed.
- New git op `git.Clone(r, url, dest) error`.
- UI: `UnclonedProjects.svelte` section under the project table, shown only when
  the list is non-empty. Each row: name, task count, remote, and a Clone button
  (disabled with a hint when `!CanClone`). Clone -> toast -> refresh list + projects.

## Sub-slice 2c-2: Local data export

- `App.ExportData() string` - open a native Save dialog (Wails runtime
  `SaveFileDialog`, default name `fleet-export-YYYY-MM-DD.json`), write the full
  store snapshot as pretty JSON, return "" on success, "cancelled" if the user
  dismisses the dialog, or an error string. Local only; no network.
- UI: a "Data" section in `SettingsModal` with an "Export data" button.

## Sub-slice 2c-3: Account deletion

- Server: `DELETE /v1/account` (auth-gated) - within one transaction, delete all
  of the caller's sync docs and revoke the caller's entire refresh-token family,
  then return 204. New `auth.DeleteAccount` handler + a pgstore method to purge a
  user's docs.
- Client: `cloud.Client.DeleteAccount(access string) error`.
- `App.DeleteAccount() string` - call the client with a fresh access token; on
  success sign out locally (drop the keychain refresh token, reset sync state).
  Return "" or an error string.
- UI: a "Delete account" item in the `AccountChip` menu (signed-in only) with a
  two-step in-menu confirm ("Delete account?" -> Cancel / Delete). Destructive
  styling.

## Error handling & safety

- Every binding follows the `errMsg` convention ("" success / "error: ...").
- Clone never overwrites an existing directory.
- Account deletion is irreversible; the UI requires an explicit second click.
  Local sign-out happens only after the server confirms deletion.

## Testing

- **Backend unit**: `SyncedUncloned` classifies git:/local:/path ids and rebuilds
  the remote; `CloneUncloned` rejects a non-git id and an existing dest (fake
  runner for the clone call). Auth `DeleteAccount` handler: deletes docs + revokes
  tokens, 401 without auth (httptest + the existing auth test harness).
- **Backend integration**: `git.Clone` against a real bare repo (skips без git).
- **Frontend**: SSR test that `UnclonedProjects` renders rows + disables Clone
  when `!CanClone`; that `SettingsModal` shows the Export control.
- **GUI**: the uncloned section + a real clone; export dialog; the delete-account
  confirm.

## Out of scope (Tier 3)

Selective per-project sync, re-clone history, exporting to formats other than
JSON, undo for account deletion.
