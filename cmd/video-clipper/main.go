// Command video-clipper downloads a full online video, or an exact time
// range from one, from any yt-dlp-supported URL. Bare invocation opens a
// local browser UI; flags make every action scriptable and headless too.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DavidMarsanic/video-clipper/internal/browser"
	"github.com/DavidMarsanic/video-clipper/engine"
	"github.com/DavidMarsanic/video-clipper/internal/paths"
	"github.com/DavidMarsanic/video-clipper/internal/server"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("video-clipper", flag.ContinueOnError)

	urlFlag := fs.String("url", "", "video URL to load (or headlessly act on with -no-browser)")
	start := fs.String("start", "", "clip start, e.g. 75, 1:15, 01:15.500, 1:02:15 (use with -end)")
	end := fs.String("end", "", "clip end (use with -start)")
	format := fs.String("format", "auto", "auto | mp4 | audio")
	quality := fs.String("quality", "best", "best | 1080p | 720p | 480p | 360p")
	output := fs.String("output", "", "output directory (default: your Downloads folder)")
	port := fs.Int("port", 0, "local UI server port (default: automatic)")
	noBrowser := fs.Bool("no-browser", false, "headless: perform the action and exit instead of opening the browser UI (requires -url)")
	jsonOut := fs.Bool("json", false, "with -no-browser, print the result as JSON")
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() { printUsage(fs) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	if *showVersion {
		fmt.Println("video-clipper " + version)
		return 0
	}

	widenPATH()

	outputDir, err := paths.ResolveDownloadsDir(*output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	eng := engine.New(outputDir)

	if *noBrowser {
		if err := eng.CheckTools(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
		return runHeadless(eng, headlessArgs{
			url: *urlFlag, start: *start, end: *end,
			format: *format, quality: *quality, outputDir: outputDir, json: *jsonOut,
		})
	}

	// UI mode never hard-fails on a missing tool here — the server still
	// starts and the browser still opens; CheckTools' error (if any) is
	// instead returned by the engine the moment the UI actually tries to
	// use it, where there's a page that can show it. A double-clicked app
	// has no terminal for a startup error to go to, so failing silently
	// before ever opening a window would be strictly worse.
	return runUI(eng, outputDir, *urlFlag, *port)
}

// widenPATH adds common tool-install directories that a GUI-launched
// process often lacks. macOS gives an app spawned outside a shell (Finder,
// Spotlight, or another GUI app like a Securexe-style launcher — anything
// that isn't a terminal) a bare PATH of /usr/bin:/bin:/usr/sbin:/sbin,
// which doesn't include wherever Homebrew actually put yt-dlp/ffmpeg. A
// plain terminal invocation already has all of this, so this only ever
// adds directories that exist on disk and aren't already present —
// nothing is removed or reordered.
func widenPATH() {
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/opt/homebrew/bin", "/opt/homebrew/sbin", // Apple Silicon Homebrew
		"/usr/local/bin", "/usr/local/sbin", // Intel Homebrew / common Linux
		filepath.Join(home, ".local", "bin"),
	}

	current := os.Getenv("PATH")
	existing := map[string]bool{}
	for _, p := range filepath.SplitList(current) {
		existing[p] = true
	}

	var toAdd []string
	for _, dir := range candidates {
		if dir == "" || existing[dir] {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			toAdd = append(toAdd, dir)
		}
	}
	if len(toAdd) == 0 {
		return
	}
	toAdd = append(toAdd, current)
	os.Setenv("PATH", strings.Join(toAdd, string(os.PathListSeparator)))
}

// runUI is bare invocation's primary action: start the loopback server and
// open the browser at it — never dump raw CLI usage to a double-clicked app.
func runUI(eng *engine.Engine, outputDir, initialURL string, port int) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(ctx, eng, outputDir)
	addr, err := srv.Start(port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	target := addr + "/"
	if initialURL != "" {
		target += "?url=" + url.QueryEscape(initialURL)
	}

	fmt.Fprintln(os.Stderr, "Video Clipper running at", addr, "— press Ctrl+C to quit")
	if err := browser.OpenAppWindow(target); err != nil {
		fmt.Fprintln(os.Stderr, "couldn't open a window automatically:", err)
		fmt.Fprintln(os.Stderr, "open this URL manually:", target)
	}

	<-ctx.Done()
	return 0
}

type headlessArgs struct {
	url, start, end, format, quality, outputDir string
	json                                        bool
}

func runHeadless(eng *engine.Engine, a headlessArgs) int {
	if a.url == "" {
		fmt.Fprintln(os.Stderr, "error: -url is required with -no-browser")
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := engine.Options{Format: a.format, Quality: a.quality, OutputDir: a.outputDir}

	onProgress := func(p engine.Progress) {
		if a.json {
			return
		}
		fmt.Fprintf(os.Stderr, "\r%-12s %5.1f%%  %s %s        ", p.Stage, p.Percent, p.Speed, p.ETA)
	}

	var result *engine.Result
	var err error

	hasRange := a.start != "" && a.end != ""
	var rangeStart, rangeEnd time.Duration
	if hasRange {
		rangeStart, rangeEnd, err = parseRange(a.start, a.end)
	}

	if err == nil {
		switch {
		case a.format == "audio" && hasRange:
			result, err = eng.ExtractAudio(ctx, a.url, &rangeStart, &rangeEnd, opts, onProgress)
		case a.format == "audio":
			result, err = eng.ExtractAudio(ctx, a.url, nil, nil, opts, onProgress)
		case hasRange:
			result, err = eng.Clip(ctx, a.url, rangeStart, rangeEnd, opts, onProgress)
		default:
			result, err = eng.Download(ctx, a.url, opts, onProgress)
		}
	}

	if !a.json {
		fmt.Fprintln(os.Stderr)
	}
	if err != nil {
		return reportError(err, a.json)
	}

	if a.json {
		data, _ := json.Marshal(result)
		fmt.Println(string(data))
	} else {
		fmt.Println(result.Path)
	}
	return 0
}

func parseRange(startStr, endStr string) (time.Duration, time.Duration, error) {
	s, err := parseTimecode(startStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid -start: %w", err)
	}
	e, err := parseTimecode(endStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid -end: %w", err)
	}
	if e <= s {
		return 0, 0, fmt.Errorf("-end must be after -start")
	}
	return s, e, nil
}

// parseTimecode accepts the same shapes as the web UI's fields: "75",
// "1:15", "01:15.500", "1:02:15".
func parseTimecode(raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty timecode")
	}
	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("invalid timecode %q", raw)
	}
	var seconds float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("invalid timecode %q", raw)
		}
		seconds = seconds*60 + v
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func reportError(err error, jsonOut bool) int {
	code := 1
	switch {
	case errors.Is(err, engine.ErrMissingDependency):
		code = 2
	case errors.Is(err, engine.ErrUnsupportedURL), errors.Is(err, engine.ErrDRMProtected),
		errors.Is(err, engine.ErrAuthRequired), errors.Is(err, engine.ErrFormatUnavailable):
		code = 3
	}
	if jsonOut {
		data, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Println(string(data))
	} else {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	return code
}

func printUsage(fs *flag.FlagSet) {
	fmt.Fprint(os.Stderr, `video-clipper — download a full video, or an exact time range from one,
from any yt-dlp-supported URL (YouTube, Vimeo, Twitter/X, Reddit, and
others). yt-dlp handles extraction; ffmpeg (invoked by yt-dlp) handles the
actual trim/remux/encode.

Bare invocation opens a local browser UI: paste a URL, preview it, drag the
timeline to pick a range, and save. Flags make every action scriptable too.

Usage:
  video-clipper                          open the browser UI
  video-clipper -url <url>                open the UI with that URL preloaded
  video-clipper -no-browser -url <url> [-start T -end T] [flags]
                                          run headlessly and exit

Flags:
`)
	fs.PrintDefaults()
	fmt.Fprint(os.Stderr, `
Exit codes:
  0  success
  1  general failure
  2  yt-dlp or ffmpeg not found on PATH
  3  unsupported URL, DRM-protected, or sign-in required

Requires yt-dlp and ffmpeg on PATH. Neither is bundled.
`)
}
