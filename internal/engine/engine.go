package engine

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Engine is the whole video-clipper backend, independent of any UI: it
// shells out to yt-dlp (extraction, downloading, and — via yt-dlp's own
// --download-sections/--force-keyframes-at-cuts — accurate section
// clipping) and relies on yt-dlp's internal use of ffmpeg for merging,
// remuxing, trimming and audio extraction. Neither tool is bundled; both
// must be on PATH.
type Engine struct {
	YtDlpPath        string
	FFmpegPath       string
	DefaultOutputDir string
}

// New resolves yt-dlp and ffmpeg on PATH and returns a ready-to-use Engine,
// or a wrapped ErrMissingDependency naming whichever tool is absent.
func New(defaultOutputDir string) (*Engine, error) {
	ytdlp, err := findYtDlp()
	if err != nil {
		return nil, err
	}
	ffmpeg, err := findFFmpeg()
	if err != nil {
		return nil, err
	}
	return &Engine{YtDlpPath: ytdlp, FFmpegPath: ffmpeg, DefaultOutputDir: defaultOutputDir}, nil
}

// Download saves the entire video (or, for Format:"audio", the entire
// audio track) to opts.OutputDir.
func (e *Engine) Download(ctx context.Context, url string, opts Options, onProgress func(Progress)) (*Result, error) {
	args := e.baseArgs(opts, nil, nil)
	return e.run(ctx, url, args, opts.OutputDir, "", onProgress)
}

// Clip saves only the [start, end) range. It never downloads the full
// source first: yt-dlp's --download-sections asks the downloader for just
// that byte/time range when the extractor supports it, and
// --force-keyframes-at-cuts re-encodes only the small boundary segments (via
// ffmpeg) so the cut lands exactly on start/end while the rest of the clip
// stays a stream copy — "exact cuts" without a blanket re-encode.
func (e *Engine) Clip(ctx context.Context, url string, start, end time.Duration, opts Options, onProgress func(Progress)) (*Result, error) {
	args := e.baseArgs(opts, &start, &end)
	return e.run(ctx, url, args, opts.OutputDir, clipLabel(start, end), onProgress)
}

// ExtractAudio saves just the audio track, optionally trimmed to [start, end).
// When the source's native audio codec is already suitable it's copied,
// not re-encoded (yt-dlp's --audio-format "best" behavior).
func (e *Engine) ExtractAudio(ctx context.Context, url string, start, end *time.Duration, opts Options, onProgress func(Progress)) (*Result, error) {
	audioOpts := opts
	audioOpts.Format = "audio"
	args := e.baseArgs(audioOpts, start, end)
	label := ""
	if start != nil && end != nil {
		label = clipLabel(*start, *end)
	}
	return e.run(ctx, url, args, opts.OutputDir, label, onProgress)
}

// baseArgs builds the shared yt-dlp argument list for all three download
// flavors; the caller adds -o/--print separately since those depend on the
// resolved output path.
func (e *Engine) baseArgs(opts Options, start, end *time.Duration) []string {
	args := []string{
		"--newline", "--no-warnings", "--no-playlist",
		"--ffmpeg-location", filepath.Dir(e.FFmpegPath),
		"-f", formatSelector(opts.Format, opts.Quality),
	}

	if opts.Format == "audio" {
		args = append(args, "-x", "--audio-format", "best")
	} else {
		args = append(args, "--merge-output-format", "mp4")
	}

	if start != nil && end != nil {
		section := fmt.Sprintf("*%s-%s", secondsArg(*start), secondsArg(*end))
		args = append(args, "--download-sections", section, "--force-keyframes-at-cuts")
	}

	return args
}

func formatSelector(format, quality string) string {
	if format == "audio" {
		return "bestaudio/best"
	}
	switch quality {
	case "1080p", "720p", "480p", "360p", "240p", "144p":
		h := strings.TrimSuffix(quality, "p")
		return fmt.Sprintf("bv*[height<=%s]+ba/b[height<=%s]", h, h)
	default: // "", "auto", "best"
		return "bv*+ba/b"
	}
}

func secondsArg(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}

func clipLabel(start, end time.Duration) string {
	fmtPart := func(d time.Duration) string {
		total := int(d.Milliseconds())
		m := total / 60000
		s := (total / 1000) % 60
		return fmt.Sprintf("%02dm%02ds", m, s)
	}
	return fmt.Sprintf("%s-%s", fmtPart(start), fmtPart(end))
}

var (
	progressRe = regexp.MustCompile(`^\[download\]\s+([\d.]+)%(?:\s+of\s+\S+)?(?:\s+at\s+(\S+))?(?:\s+ETA\s+(\S+))?`)
	finalPathRe = regexp.MustCompile(`^FINALPATH:(.+)$`)
	processingRe = regexp.MustCompile(`^\[(Merger|ExtractAudio|VideoRemuxer|Fixup|VideoConvertor)]`)
)

// maxAttempts covers a real, observed failure mode: yt-dlp's ffmpeg-backed
// section downloader intermittently fails a clip (e.g. "ffmpeg exited with
// code 8") when googlevideo's CDN drops or rejects one of the two stream
// requests mid-fetch, even though the exact same request reliably succeeds
// moments later. That's CDN flakiness, not a bad selector or a bad URL, so
// it's worth one or two retries before surfacing it as a real failure.
const maxAttempts = 3

// run retries attemptOnce, skipping the retry for errors that are never
// going to succeed on a second try (unsupported URL, DRM, auth, a truly
// missing format/tool).
func (e *Engine) run(ctx context.Context, url string, args []string, outputDir, label string, onProgress func(Progress)) (*Result, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := e.attemptOnce(ctx, url, args, outputDir, label, onProgress)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if errors.Is(err, ErrUnsupportedURL) || errors.Is(err, ErrDRMProtected) ||
			errors.Is(err, ErrAuthRequired) || errors.Is(err, ErrFormatUnavailable) ||
			errors.Is(err, ErrMissingDependency) {
			return nil, err
		}
		if attempt < maxAttempts {
			if onProgress != nil {
				onProgress(Progress{Stage: "downloading", Percent: 0, Message: "network hiccup, retrying…"})
			}
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, lastErr
}

// attemptOnce executes yt-dlp once with an output template + a print
// directive that unambiguously reports the final file path once all
// postprocessing is done, streaming coarse progress to onProgress as it goes.
func (e *Engine) attemptOnce(ctx context.Context, url string, args []string, outputDir, label string, onProgress func(Progress)) (*Result, error) {
	template := "%(title)s"
	if label != "" {
		template = fmt.Sprintf("%%(title)s [%s]", label)
	}
	template += ".%(ext)s"
	outTemplate := filepath.Join(outputDir, template)

	fullArgs := append(append([]string{}, args...),
		"-o", outTemplate,
		"--print", "after_move:FINALPATH:%(filepath)s",
		url,
	)

	cmd := exec.CommandContext(ctx, e.YtDlpPath, fullArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("starting yt-dlp: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// yt-dlp's ffmpeg-backed downloader — used whenever --download-sections
	// is in play, i.e. every Clip/ExtractAudio-with-range call — never
	// prints incremental [download] percentage lines at all (ffmpeg itself
	// runs with -loglevel quiet under the hood), so a long, high-bitrate
	// section fetch can go a minute or more with zero output even though
	// it's working fine. A ticker re-publishes the last known state on a
	// fixed cadence so the SSE stream — and anything watching it for a
	// stall — never sees real silence, whether or not yt-dlp itself is
	// saying anything.
	var mu sync.Mutex
	last := Progress{Stage: "resolving", Percent: 0}
	publish := func(p Progress) {
		mu.Lock()
		last = p
		mu.Unlock()
		if onProgress != nil {
			onProgress(p)
		}
	}
	publish(last)

	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				p := last
				mu.Unlock()
				if onProgress != nil {
					onProgress(p)
				}
			case <-heartbeatDone:
				return
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting yt-dlp: %w", err)
	}
	// Section downloads (any Clip/ranged ExtractAudio call) go through
	// yt-dlp's ffmpeg-backed downloader, which prints no [download]
	// percentage lines at all — so once the process is actually running,
	// call it "downloading" rather than leaving it reading "resolving" for
	// what can be a minute or more on a long, high-quality clip.
	publish(Progress{Stage: "downloading", Percent: 0})

	var finalPath string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case finalPathRe.MatchString(line):
			finalPath = finalPathRe.FindStringSubmatch(line)[1]
		case progressRe.MatchString(line):
			m := progressRe.FindStringSubmatch(line)
			p := Progress{Stage: "downloading", Speed: m[2], ETA: m[3]}
			fmt.Sscanf(m[1], "%f", &p.Percent)
			publish(p)
		case processingRe.MatchString(line):
			publish(Progress{Stage: "processing", Percent: 100})
		}
	}

	waitErr := cmd.Wait()
	if waitErr != nil {
		return nil, classifyError(stderr.String(), waitErr)
	}
	if finalPath == "" {
		return nil, fmt.Errorf("yt-dlp reported success but no output path: %s", errorDetail(stderr.String()))
	}

	// Deliberately no "done" progress event here: this layer doesn't know
	// the caller-facing Result yet (path/filename are computed by the
	// caller from what we return). The one authoritative "done" — with
	// the real path — is published by internal/server's job handler right
	// after this call returns. Publishing one here too would race it: SSE
	// consumers close the stream on the first "done" they see, so a
	// path-less one from this layer would win and the real one would
	// never be delivered.
	return &Result{Path: finalPath, Filename: filepath.Base(finalPath)}, nil
}

func classifyError(stderr string, cause error) error {
	s := strings.ToLower(stderr)
	detail := errorDetail(stderr)
	switch {
	case strings.Contains(s, "unsupported url"):
		return fmt.Errorf("%w: %s", ErrUnsupportedURL, detail)
	case strings.Contains(s, "drm"):
		return fmt.Errorf("%w: %s", ErrDRMProtected, detail)
	case strings.Contains(s, "sign in") || strings.Contains(s, "login required") ||
		strings.Contains(s, "private video") || strings.Contains(s, "premium") ||
		strings.Contains(s, "cookies"):
		return fmt.Errorf("%w: %s", ErrAuthRequired, detail)
	case strings.Contains(s, "requested format is not available"):
		return fmt.Errorf("%w: %s", ErrFormatUnavailable, detail)
	case detail == "":
		return fmt.Errorf("yt-dlp: %w", cause)
	default:
		return fmt.Errorf("yt-dlp: %s", detail)
	}
}

// errorDetail pulls a useful diagnostic out of yt-dlp's stderr. yt-dlp
// often summarizes a failure in one terse final line (e.g. "Postprocessing:
// ffmpeg exited with code 8") while the actual reason was printed a line
// or two earlier by the ffmpeg subprocess it shelled out to — so this
// keeps the last few non-empty lines, not just the last one.
func errorDetail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	var kept []string
	for i := len(lines) - 1; i >= 0 && len(kept) < 6; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		kept = append([]string{strings.TrimPrefix(line, "ERROR: ")}, kept...)
	}
	return strings.Join(kept, " | ")
}
