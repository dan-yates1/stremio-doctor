# stremio-doctor

A Go CLI that discovers a user's installed Stremio addons, probes them from the user's network, and explains slowness or failures. Public repo (MIT). Releases ship as single binaries per OS so non-technical users can double-click them.

## Layout
- `cmd/stremio-doctor`: flags, orchestration (`scanOnce` = discover → scan → `report.Build`, shared by one-shot and watch runs), double-click behaviour (`console_windows.go` keeps the window open and enables ANSI colour). `watch.go` drives `--watch`: per-cycle summary lines, status-change lines, history recording.
- `internal/discover`: addon sources, tried in order: `--addon`/`--addons-file`, `--auth-key`, then auto-read of the Stremio app's Chromium localStorage leveldb (`paths.go` holds the per-OS globs; `localstorage.go` copies the dir to temp and reads the `profile` key). `api.go` calls `addonCollectionGet` (read-only).
- `internal/probe`: a single timed GET with the httptrace phase breakdown, plus error classification.
- `internal/scan`: per-addon checks (manifest, catalog, meta, stream × 2 samples, subtitles) over N rounds, plus baseline targets.
- `internal/diagnose`: per-addon rules (`diagnose.go`) and cross-addon rules (`global.go`: duplicates, shared host, slowest stream addons). Thresholds live in `diagnose.Thresholds`.
- `internal/report`: `Build` assembles and redacts the report; terminal, HTML (`report.html.tmpl`, embedded) and JSON writers. `Report.History` (a `HistoryView`) is optional and filled by the caller.
- `internal/history`: JSONL file of per-scan summaries (`Entry`, default `os.UserConfigDir()/stremio-doctor/history.jsonl`), `Transitions` (status changes between scans) and `View` (the report's History section). Every run appends; `Prune` drops entries older than `--keep-days`.
- `internal/watch`: `Watcher` loop: scans now, then every interval after the previous scan ends; `ScanNow`/`SetPaused` are goroutine-safe hooks (for the tray).

## Rules
- **Secrets:** addon URLs embed debrid keys. Reports must redact them unless `--show-urls` is set (`report.Redact`). `probe.Classify` strips URLs from error messages. The auth key is only sent to api.strem.io and never printed or written. `TestReportsNeverLeakSecretsByDefault` guards this. History entries never hold URLs, even with `--show-urls` (`TestSummarizeNeverStoresURLs`).
- Never write to the user's Stremio data or call `addonCollectionSet`.
- Keep dependencies minimal: stdlib plus goleveldb.
- CI and release build with `go-version: stable`, not go.mod: go1.22.0 produces macOS binaries without LC_UUID that crash on current macOS.

## Commands
- `go test ./...`, `go vet ./...`
- `go run ./cmd/stremio-doctor --addon https://v3-cinemeta.strem.io/manifest.json`
- Release: push a `v*` tag; GoReleaser (`.goreleaser.yaml`) builds windows/darwin/linux × amd64/arm64.

Go isn't on PATH in this dev environment's shell; use `C:\Program Files\Go\bin\go.exe`.
