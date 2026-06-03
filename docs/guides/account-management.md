# Account Management

Gas Town supports multiple Claude Code accounts (e.g., personal, work, team)
with per-session override and a global default.

## Configuration

Accounts are registered in `~/gt/mayor/accounts.json`:

```json
{
  "version": 1,
  "accounts": {
    "personal": {
      "email": "user@gmail.com",
      "description": "Personal account",
      "config_dir": "/Users/you/.claude-accounts/personal"
    },
    "work": {
      "email": "user@company.com",
      "description": "Work account",
      "config_dir": "/Users/you/.claude-accounts/work"
    }
  },
  "default": "personal"
}
```

Each account gets its own Claude Code config directory under `~/.claude-accounts/<handle>/`.

## Account Resolution Order

When spawning a session, the account is resolved in this order (highest wins):

1. **`GT_ACCOUNT` environment variable**
2. **`--account` flag** on the command
3. **`default`** from `accounts.json`

The resolved account's `config_dir` is injected as `CLAUDE_CONFIG_DIR` into the
spawned session.

## Commands

### `gt account list`

List all registered accounts. The default is marked with `*`.

```bash
gt account list           # Text output
gt account list --json    # JSON output
```

### `gt account add <handle>`

Register a new account. Creates `~/.claude-accounts/<handle>/` and symlinks
global commands (e.g., from `~/.claude/commands`) into it.

```bash
gt account add work
gt account add work --email user@company.com
gt account add work --email user@company.com --desc "Work account"
```

The first account added automatically becomes the default.

After adding, complete login:

```bash
CLAUDE_CONFIG_DIR=~/.claude-accounts/work claude
# Then use /login inside Claude Code
```

### `gt account default <handle>`

Set which account is used when no override is specified.

```bash
gt account default work
```

### `gt account status`

Show the currently resolved account and how it was resolved.

```bash
gt account status
GT_ACCOUNT=work gt account status   # Shows env override
```

### `gt account switch <handle>`

Switch the active account by managing `~/.claude` as a symlink:

1. If `~/.claude` is a real directory, moves it to the current account's `config_dir`
2. Creates a symlink: `~/.claude` -> target account's `config_dir`
3. Updates the default in `accounts.json`

```bash
gt account switch work
```

Requires a Claude Code restart to take effect.

## Per-Session Overrides

### `--account` flag

Available on `gt sling`, `gt crew at`, and `gt crew start`:

```bash
gt sling gp-abc greenplace --account work
gt crew at --account work max
gt crew start --account work
```

### `GT_ACCOUNT` environment variable

Overrides everything, including the `--account` flag:

```bash
export GT_ACCOUNT=work
gt sling gp-abc greenplace   # Uses work account
```

## Per-Rig Account Settings

A rig's `settings/config.json` can assign accounts by role or by individual
worker, so agents started without an explicit `--account` flag still land on the
right account. This is how Claude Code spend is distributed (e.g. polecats on one
account, persistent crew on another) without anyone passing a flag every time.

```json
{
  "type": "rig-settings",
  "default_account": "personal",
  "role_accounts":   { "polecat": "personal", "crew": "qconcierge" },
  "worker_accounts": { "woodhouse": "personal" }
}
```

- `default_account` — rig-wide fallback for every agent in the rig.
- `role_accounts[role]` — per-role override. Roles: `crew`, `polecat`,
  `witness`, `refinery`, `mayor`.
- `worker_accounts[name]` — per-named-crew override (highest of the three).

### Resolution priority (highest wins)

1. `GT_ACCOUNT` env var
2. `--account` flag
3. `worker_accounts[worker]` (rig settings)
4. `role_accounts[role]` (rig settings)
5. `default_account` (rig settings)
6. `accounts.json` global default

These rig settings are honored by `gt crew start`, `gt start`, `gt crew at`,
and polecat spawning (including `gt sling`, which resolves
`role_accounts["polecat"]` at spawn time when no `--account` is given).

## Shared Commands

When adding an account, `gt account add` symlinks
`~/.claude/commands` into the new account's config directory so custom commands
(e.g., SuperClaude) are available regardless of which account is active.
