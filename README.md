# stremio-doctor

**Find out which of your Stremio addons is slow or broken, and why.**

Streams won't load? The list keeps spinning? stremio-doctor finds the addons you have installed, tests each one from your own computer the same way Stremio calls them, and tells you in plain English what's wrong:

```
  Addon                    Host                         manifest    catalog       meta     stream  subtitles  streams
✗ OldAddon                 old-addon.example.com            FAIL          -          -          -          -        -
! Torrentio                torrentio.strem.fun              60ms          -          -       6.8s          -       42
✓ Cinemeta                 v3-cinemeta.strem.io             97ms      139ms       94ms          -          -        -

Findings
  [FAIL] OldAddon: Domain doesn't resolve
         The addon's domain name doesn't exist anymore. The addon has likely shut down or moved; reinstall it.
  [WARN] Torrentio: Slow stream
         stream movie tt0111161 takes 6.8s (median). 6.8s: network 0.2s, server 6.4s, download 0.2s.
         Most of that is the addon's server working, so the addon is the bottleneck, not your connection.
```

It also writes a shareable HTML report with a timing breakdown for every request.

## How is this different from Stremio Status?

[Stremio Status](https://github.com/SolitudePy/stremio-status) is great for checking whether a popular addon is down for everyone. stremio-doctor answers a different question: what's wrong with my setup?

| | Stremio Status | stremio-doctor |
|---|---|---|
| Which addons | A fixed list of popular public addons | The addons **you** have installed, including your configured/private/self-hosted instances |
| Measured from | Its own servers | **Your** computer and network |
| Detail | Up / down + latency | Per request type (manifest, catalog, meta, stream, subtitles), split into DNS / connect / TLS / server / download |
| Diagnosis | n/a | Timeouts, dead domains, rate limits (429), Cloudflare blocks, bad debrid keys (401/403), flaky or inconsistent addons, duplicates, several addons failing on the same host |

Use both: if Stremio Status says an addon is up but stremio-doctor says it's slow for you, the problem is between you and it.

## Download and run

**Non-technical users:** download the file for your system from [Releases](https://github.com/dan-yates1/stremio-doctor/releases), double-click it, and the report opens in your browser.

- Windows: `stremio-doctor_windows_amd64.zip` → `stremio-doctor.exe`. If SmartScreen warns about an unrecognised app, click *More info → Run anyway*. The binaries aren't code-signed yet.
- macOS: `stremio-doctor_darwin_arm64.tar.gz` (Apple Silicon) or `_amd64` (Intel). Run it from Terminal the first time: `xattr -d com.apple.quarantine stremio-doctor && ./stremio-doctor`.
- Linux: `stremio-doctor_linux_amd64.tar.gz`.

**With Go installed:**

```bash
go install github.com/dan-yates1/stremio-doctor/cmd/stremio-doctor@latest
```

## How it finds your addons

In order, the first source that works wins:

1. `--addon <url>` / `--addons-file <file>`: test specific addon manifest URLs.
2. `--auth-key <key>` or `STREMIO_AUTH_KEY`: fetch your addon list from your Stremio account.
3. **Automatic:** read your login from the Stremio desktop app on this computer (Stremio 5 and 4 on Windows, Stremio 4 on macOS/Linux). The app's data is copied to a temp folder and read from there; the original is never modified. If you're logged in, the live addon list is fetched from your account; otherwise it uses the app's local copy.

Use Stremio in a browser instead? Get your auth key: open [web.stremio.com](https://web.stremio.com), log in, press F12, and in the Console run:

```js
JSON.parse(localStorage.getItem("profile")).auth.key
```

## Options

| Flag | Default | |
|---|---|---|
| `--rounds` | `3` | Times each request is repeated (results use the median) |
| `--timeout` | `15s` | Per-request timeout |
| `--concurrency` | `8` | Addons tested at once |
| `--html <file>` | `stremio-doctor-report.html` | HTML report path (`""` to skip) |
| `--json <file>` | | Also write JSON (`-` for stdout) |
| `--no-open` | | Don't open the report in the browser |
| `--show-urls` | | Show full addon URLs (**may contain your API keys**) |
| `--profile-dir` | | Stremio `Local Storage/leveldb` folder, if auto-detection misses it |
| `--watch` | | Keep running and rescan every `--interval` |
| `--tray` | | Watch from a system tray icon (implies `--watch`) |
| `--interval` | `15m` | Time between scans in watch mode (minimum `1m`) |
| `--history <file>` | see below | Where scan summaries are kept (`""` to disable) |
| `--keep-days` | `30` | Days of history to keep |
| `--share` | | Opt in to sending anonymous results for well-known public addons to a community status page (not live yet) |
| `--share-dry-run` | | Print exactly what `--share` would send, without sending anything |

## Watch mode and history

Slowness that comes and goes is hard to catch with one test. Run:

```bash
stremio-doctor --watch
```

It rescans every 15 minutes, prints one line per scan, and calls out any addon whose status changed (`Torrentio: ok → fail`). The HTML report is rewritten after every scan and gains a **History** section showing each addon's status over recent scans, its uptime and its typical stream time.

Every run, watched or not, appends a short summary to a history file in your user config folder (`%AppData%\stremio-doctor\history.jsonl` on Windows, `~/Library/Application Support/stremio-doctor/` on macOS, `~/.config/stremio-doctor/` on Linux). It holds addon names, hosts, statuses and timings, never addon URLs or keys, and it stays on your computer. Entries older than `--keep-days` are dropped.

### Tray icon

`stremio-doctor --tray` runs the same watch loop behind an icon in the system tray (menu bar on macOS). The icon is green, amber or red for the worst addon status. Its menu shows the latest result and the addons with problems, and has **Scan now**, **Open report**, **Pause watching** and **Quit**.

To use it without a terminal, make a shortcut with the flag added:

- **Windows:** right-click `stremio-doctor.exe` → *Create shortcut*, open the shortcut's *Properties*, and add ` --tray` to the end of *Target*. To start it with Windows, put the shortcut in the folder that `Win+R` → `shell:startup` opens.
- **macOS / Linux:** run `stremio-doctor --tray &`, or add that command to your login items / autostart. On Linux the desktop needs a StatusNotifierItem tray (KDE, or GNOME with the AppIndicator extension).

## What gets tested

For each addon, every round:

- `manifest.json`
- the first catalog that needs no search/filter input
- `meta` for the first item that catalog returned, or for a sample movie
- `stream` for *The Shawshank Redemption* (`tt0111161`) and *Game of Thrones* S01E01 (`tt0944947:1:1`), when the addon supports them
- `subtitles` for the sample movie

Each request uses a fresh connection, so every result has a full DNS → connect → TLS → server → download breakdown. It also checks three reference points: general internet (Cloudflare), Stremio's API, and Stremio's local streaming server.

## Privacy

- Addon URLs often contain debrid API keys. **Reports redact them by default** (`https://host/…/manifest.json`), so you can paste reports into GitHub issues or Reddit.
- Your Stremio auth key is only ever sent to `api.strem.io` (the official API), and only to *read* your addon list. It is never printed, logged, or written to disk. The tool never changes your addons.
- Nothing is sent anywhere else unless you pass `--share`. Without it, the only network traffic is to your own addons and the three reference endpoints above.
- `--share` is opt-in. It only includes addons served from a short built-in list of well-known public instances (Cinemeta, Torrentio, the shared ElfHosted instances and similar; see `internal/share/hosts.go`). Configured, private and self-hosted addons are never sent. For those public addons it sends the manifest id, version and host, plus per-request median time, failure count and error type. It also sends your internet and Stremio API response times, so a slow connection isn't blamed on the addon, and the scan hour in UTC. It never sends addon URLs, keys, names of your other addons, IP addresses or your auth key. Run `--share-dry-run` to see the exact JSON. No community server exists yet, so `--share` currently sends nothing.
- The history file (see [Watch mode and history](#watch-mode-and-history)) stays on your computer and never contains addon URLs.

## Building from source

```bash
go test ./...
go build ./cmd/stremio-doctor
```

On macOS the tray needs cgo, so building there needs the Xcode command line tools (`xcode-select --install`). Windows and Linux builds are pure Go.

Releases are built by [GoReleaser](https://goreleaser.com) on a macOS runner when a `v*` tag is pushed.

## Licence

MIT
