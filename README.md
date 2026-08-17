# Video Clipper

Download a full online video, or an exact time range from one, from any
[yt-dlp](https://github.com/yt-dlp/yt-dlp)-supported URL — YouTube, Vimeo,
Twitter/X, Reddit, and hundreds of others.

No accounts, no library, no settings to hand-tune. Paste a URL (or let it
auto-load from Securexe's current-context), preview it, drag a timeline to
pick a range, save. Done.

## Requirements

Two external tools, both expected on `PATH` — neither is bundled:

- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp#installation) — extraction
- [`ffmpeg`](https://ffmpeg.org/download.html) — trimming, remuxing, encoding
  (yt-dlp invokes it internally for merges, accurate section clips, and
  audio extraction)

On macOS: `brew install yt-dlp ffmpeg`.

## Build

```sh
go build -o video-clipper ./cmd/video-clipper
```

Zero third-party Go dependencies — the whole HTTP UI layer runs on
`net/http`. Cross-compiles cleanly for linux/macOS/Windows with
`CGO_ENABLED=0`.

## Use

Bare invocation opens a local browser UI — this is the primary, everyday
way to use the tool:

```sh
./video-clipper                 # opens the UI; focuses the URL field
./video-clipper -url <url>      # opens the UI with that URL preloaded
```

Everything is also scriptable, for automation or when there's no browser
to hand:

```sh
./video-clipper -no-browser -url <url> -start 1:15 -end 2:51
./video-clipper -no-browser -url <url> -format audio -json
```

Start/end accept `75`, `1:15`, `01:15.500`, or `1:02:15`. Full flag
reference: `./video-clipper -h`.

Exit codes: `0` success · `1` general failure · `2` yt-dlp/ffmpeg missing ·
`3` unsupported URL, DRM-protected, or sign-in required.

## macOS app

`packaging/macos/video-clipper.app` follows Securexe's double-clickable-app
convention (`Info.plist` + `launch.sh` that execs `video-clipper-bin` next
to itself), so Securexe's builder picks it up automatically. To install it
by hand for local testing:

```sh
go build -o video-clipper ./cmd/video-clipper
cp -R packaging/macos/video-clipper.app "/Applications/Video Clipper.app"
cp video-clipper "/Applications/Video Clipper.app/Contents/MacOS/video-clipper-bin"
```

It then launches from Spotlight like any other app.

## How it works

- **yt-dlp** inspects the URL (title, duration, thumbnail, uploader,
  available formats) and handles all extraction/downloading — no
  site-specific logic lives in this tool.
- **Clipping never downloads the full source first.** yt-dlp's
  `--download-sections` fetches only the requested range where the site
  supports it, and `--force-keyframes-at-cuts` re-encodes (via ffmpeg) just
  the small boundary segments so the cut lands exactly on the requested
  start/end — the rest of the clip stays a stream copy.
- The UI is a plain HTML/CSS/JS single page, embedded in the Go binary via
  `go:embed` and served from a `127.0.0.1`-only HTTP server — no build
  step, no framework, never reachable off-host.
- The video preview streams through a small same-origin proxy
  (`/api/preview`) rather than pointing `<video>` straight at the CDN URL
  yt-dlp resolved: sites like YouTube reject the open-ended range requests
  a browser makes against a large file, but happily serve bounded chunks —
  the proxy always requests bounded ranges upstream regardless of what the
  browser asks for, so preview works the same for a 3-minute video and a
  3-hour one.
- A brief CDN hiccup on a clip's boundary fetch (occasionally seen as an
  ffmpeg failure from yt-dlp's section downloader) is retried automatically
  before being surfaced as a real error.

The engine is UI-agnostic (`internal/engine`): `Inspect`, `Download`,
`Clip`, `ExtractAudio`. The HTTP layer (`internal/server`) is one consumer
of it; the `-no-browser` CLI path is another.

### Securexe context

No concrete Securexe current-context API is documented anywhere this
project has access to, so `internal/server/context.go` is a small,
isolated, best-effort adapter: it checks `SECUREXE_CONTEXT_URL` /
`SECUREXE_URL` / `SECUREXE_FRONTMOST_APP` / `SECUREXE_WINDOW_TITLE` env
vars, plus a `SECUREXE_CONTEXT_JSON` env var
(`{"url","frontmostApp","windowTitle"}`). If none are set, the UI just
focuses the URL field — nothing is required. Swapping in Securexe's real
API is a one-file change.

## License

MIT — see [LICENSE](LICENSE).
