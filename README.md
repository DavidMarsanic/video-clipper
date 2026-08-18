# Video Clipper

Download a full online video, or an exact time range from one, from any
YouTube, Vimeo, Twitter/X, Reddit, or other supported URL.

No accounts, no library, no settings to hand-tune. Paste a URL, preview it,
drag a timeline to pick a range, save. Done.

Opens as its own window — no browser tabs or address bar.

## Requirements

Two external tools, both expected on `PATH` — neither is bundled:

- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp#installation) — extraction
- [`ffmpeg`](https://ffmpeg.org/download.html) — trimming, remuxing, encoding

On macOS: `brew install yt-dlp ffmpeg`. On Windows: grab both from their
own installers/release pages and make sure they end up on `PATH`.

If either is missing, the app still opens — it'll tell you what's missing
the moment you try to use it, rather than failing silently on launch.

## Use

Bare invocation opens its own window — this is the primary, everyday way
to use the tool. Paste a URL, wait for the preview to load, then either:

- **Download the whole thing**, or
- **Drag the timeline** to pick a start/end range and clip just that part.

Pick a quality and format (video or audio-only), then save. Clipping
doesn't download the full source first where the site supports it — only
the requested range is fetched.

It's also scriptable for automation or when there's no browser to hand —
run `video-clipper -h` for the full flag reference.

## License

MIT — see [LICENSE](LICENSE).
