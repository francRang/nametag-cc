# Agent Context

This file captures the architecture, explicit decisions, and conventions for this
codebase. Read it before making changes.

## What this program does

`nametag` is a self-updating CLI written in Go. On startup it:
1. Prints its current version (`"Awesome program version: x.y.z"`)
2. Calls the GitHub Releases API to check for a newer semver tag
3. If one exists: downloads the platform binary, verifies its SHA-256 checksum,
   atomically replaces the running binary, and restarts the process
4. If not: continues running normally

The update check happens once at startup. There is no background polling.

## Architecture

```
cmd/nametag/
  main.go                 — entry point: print version, check for update, run
  cleanup_windows.go      — Windows-only init() that removes the .old backup binary

internal/version/
  version.go              — Version var (set via ldflags), String(), IsNewer()
  version_test.go

internal/updater/
  updater.go              — Config, Updater, CheckAndUpdate, apply, download, verify
  github.go               — GitHub Releases API client, parseChecksums
  replace_unix.go         — replaceBinary (atomic rename) + restart (syscall.Exec)
  replace_windows.go      — replaceBinary (.old rename trick) + restart (exec + os.Exit)
  updater_test.go
```

## Explicitly decided — do not revisit without good reason

**Naming**
- Use `"binary"` everywhere. Never use `"exe"` or `"executable"` as variable names
  or in comments. `"exe"` carries Windows connotations; `"executable"` is ambiguous.
  The one exception is the `.exe` file extension in `platformAssetName()` — that is
  a literal filename convention, not a word choice.
- `os.Executable()` is a Go stdlib call and cannot be renamed. Store its result in
  a variable named `bin`, not `exe`.

**Background polling**
- `RunBackground` polls on a ticker after the startup check. The interval is
  controlled by the `-interval` flag (default 1h). Accepts any `time.Duration`
  string: `30s`, `5m`, `1h`, etc.

**No no-ops in the `updater` package**
- Windows startup cleanup (`*.old` removal) lives in `cmd/nametag/cleanup_windows.go`
  as a build-constrained `init()`. The `updater` package has no Windows-specific
  exported symbols and no no-op stubs. Do not move cleanup back into `updater`.

**No config file**
- All configuration is injected at build time via `-ldflags`. There is no config
  file, no environment variables, and no flags. Keep it that way.

**No extra dependencies**
- The only external dependency is `golang.org/x/mod` (for `semver`). Do not add
  third-party packages without a strong justification.

**Install to user-owned directories**
- Self-update requires write access to the binary's location. The recommended
  install path is `~/.local/bin`. Never tell users to install to `/usr/local/bin`
  or any other system directory — the update will silently fail with a permission
  error at runtime.

**`"dev"` builds never auto-update**
- `version.IsNewer` returns `(false, nil)` when `current == "dev"`. This is a
  deliberate guard to prevent developers from overwriting their local build.
  Build with a real version (`make build VERSION=1.0.0`) to test the update path.

## Platform behaviour

| | Linux / macOS | Windows |
|---|---|---|
| Replace | `os.Chmod` + atomic `os.Rename` | Rename current to `.old`, rename new into place |
| Restart | `syscall.Exec` (same PID, in-place) | `exec.Command` + `os.Exit(0)` (new PID) |
| Cleanup | Nothing to clean up | `init()` in `cleanup_windows.go` removes `.old` |

## Testing conventions

- Tests are in package `updater` (white-box), not `updater_test`, so they can
  access `replaceFn` and `restartFn` directly.
- All HTTP is intercepted via `httptest.Server` + `redirectingClient`, which
  rewrites the host on every request. Real URL-building code runs unchanged.
- `replaceFn` and `restartFn` are plain function fields on `Updater`, swapped for
  no-op stubs in tests. No mock framework, no interfaces.
- Always run tests with `-race`: `go test -race -count=1 ./...`

## Releasing

Create a release through the GitHub UI. The CI workflow (`ci.yml`) automatically:
1. Runs tests on Linux, macOS, and Windows
2. Cross-compiles all five platform binaries
3. Generates `checksums.txt`
4. Attaches everything to the release

Do not create the release manually through the UI *and* let CI run — the
`softprops/action-gh-release` action will conflict with an existing release.
Either let CI create it, or create it manually after disabling the release job.

## Code style

Follow the Uber Go style guide (https://github.com/uber-go/guide/blob/master/style.md).
- Comments explain *why*, not *what*
- Errors wrapped with `fmt.Errorf("context: %w", err)`
- No comment that merely restates the function signature
