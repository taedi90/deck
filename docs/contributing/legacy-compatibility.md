# Legacy Compatibility

`deck` still carries a small set of compatibility fallbacks for pre-XDG paths and older on-disk state formats. Keep these shims narrow, tested, and easy to delete.

## Compatibility Matrix

| Area | Current path | Legacy fallback | Code | Removal gate |
|---|---|---|---|---|
| Server remote defaults | `XDG_CONFIG_HOME/deck/server.json` | `~/.deck/server.json` | `cmd/deck/source_defaults_compat.go` | Remove after a documented migration command or release note explicitly drops `~/.deck/server.json` support |
| CLI cache root discovery | `XDG_CACHE_HOME/deck/` | `~/.deck/cache/` | `cmd/deck/cache_compat.go` | Remove after cache inspection/cleanup commands no longer need to surface legacy roots |
| Prepare package cache state | `XDG_CACHE_HOME/deck/state/<sha>.json` | `~/.deck/cache/state/<sha>.json` | `internal/prepare/cache_compat.go` | Remove after one release cycle with migration notes for users carrying old prepared cache state |
| Apply state read path | `XDG_STATE_HOME/deck/state/<key>.json` | `~/.deck/state/<key>.json` | `internal/install/state_compat.go` | Remove after one release cycle with migration notes for existing apply state files |
| Apply state JSON shape | phase-based state file | legacy step-based state payload | `internal/install/state.go` | Remove only after legacy state files are no longer read in supported upgrades |

## Rules

- Add a compatibility shim only when it preserves a real upgrade path.
- Put compatibility reads in a dedicated `*_compat.go` file when practical.
- Keep default-path tests and compatibility-path tests separate.
- Prefer read-only fallback behavior. New writes should go to the canonical path.
- Document the removal gate when adding a new shim.

## Test Coverage

- `cmd/deck/source_defaults_compat_test.go`
- `internal/install/state_compat_test.go`
- `internal/prepare/cache_compat_test.go`

## Removal Process

1. Confirm the documented removal gate has been met.
2. Delete the shim and its compatibility tests.
3. Update docs and release notes in the same change.
4. Run `make test && make lint` and any affected migration/generation checks.
