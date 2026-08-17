package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// ytdlpFormat mirrors the fields we need from one entry of yt-dlp -J's
// "formats" array. yt-dlp emits many more fields; we only decode what we use.
type ytdlpFormat struct {
	FormatID   string  `json:"format_id"`
	Ext        string  `json:"ext"`
	URL        string  `json:"url"`
	VCodec     string  `json:"vcodec"`
	ACodec     string  `json:"acodec"`
	Protocol   string  `json:"protocol"`
	Height     int     `json:"height"`
	FormatNote string  `json:"format_note"`
	Resolution string  `json:"resolution"`
	Filesize   float64 `json:"filesize"`
}

// ytdlpInfo mirrors the subset of yt-dlp -J's top-level metadata we use.
type ytdlpInfo struct {
	Title      string        `json:"title"`
	Duration   float64       `json:"duration"`
	Thumbnail  string        `json:"thumbnail"`
	Uploader   string        `json:"uploader"`
	Channel    string        `json:"channel"`
	Extractor  string        `json:"extractor_key"`
	WebpageURL string        `json:"webpage_url"`
	IsLive     bool          `json:"is_live"`
	Formats    []ytdlpFormat `json:"formats"`
}

var standardHeights = []int{2160, 1440, 1080, 720, 480, 360, 240, 144}

// Inspect runs `yt-dlp -J` against url and shapes the result into a VideoInfo.
// It never downloads or extracts a full playlist (--no-playlist): this tool
// clips one video at a time.
func (e *Engine) Inspect(ctx context.Context, rawURL string) (*VideoInfo, error) {
	if e.toolsErr != nil {
		return nil, e.toolsErr
	}
	cmd := exec.CommandContext(ctx, e.YtDlpPath, "-J", "--no-playlist", "--no-warnings", rawURL)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, classifyError(stderr.String(), err)
	}

	var info ytdlpInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("parsing yt-dlp metadata: %w", err)
	}
	return buildVideoInfo(&info), nil
}

func buildVideoInfo(info *ytdlpInfo) *VideoInfo {
	uploader := info.Uploader
	if uploader == "" {
		uploader = info.Channel
	}

	haveHeight := map[int]bool{}
	formats := make([]FormatInfo, 0, len(info.Formats))
	var previewURL string
	var previewScore = -1 // higher is better; picks the best progressive candidate

	for _, f := range info.Formats {
		hasVideo := f.VCodec != "" && f.VCodec != "none"
		hasAudio := f.ACodec != "" && f.ACodec != "none"

		formats = append(formats, FormatInfo{
			FormatID:   f.FormatID,
			Ext:        f.Ext,
			Resolution: f.Resolution,
			VCodec:     f.VCodec,
			ACodec:     f.ACodec,
			Note:       f.FormatNote,
			Filesize:   int64(f.Filesize),
		})

		if hasVideo {
			for _, h := range standardHeights {
				if f.Height >= h {
					haveHeight[h] = true
					break
				}
			}
		}

		// Prefer a true progressive (muxed video+audio, plain-HTTP) direct
		// URL for the browser <video> preview: only plain http/https here,
		// never HLS/DASH protocols — those report vcodec+acodec too but
		// Chrome/Firefox/Arc can't play an .m3u8 manifest natively (only
		// Safari can), so scoring them in by height alone would rank a
		// 1080p HLS manifest above a 360p file that Chrome can actually
		// play. Fall back to HLS below only if no progressive URL exists.
		if f.URL != "" && hasVideo && hasAudio && (f.Protocol == "https" || f.Protocol == "http") {
			score := f.Height
			if score > 720 {
				score = 720 // a moderate resolution loads faster for a preview
			}
			if score > previewScore {
				previewScore = score
				previewURL = f.URL
			}
		}
	}
	if previewURL == "" {
		for _, f := range info.Formats {
			if f.URL != "" && (f.Protocol == "m3u8" || f.Protocol == "m3u8_native") {
				previewURL = f.URL
				break
			}
		}
	}

	qualities := []string{}
	if len(formats) > 0 {
		qualities = append(qualities, "best")
	}
	for _, h := range standardHeights {
		if haveHeight[h] {
			qualities = append(qualities, fmt.Sprintf("%dp", h))
		}
	}

	return &VideoInfo{
		Title:      info.Title,
		Duration:   info.Duration,
		Thumbnail:  info.Thumbnail,
		Uploader:   uploader,
		Site:       info.Extractor,
		WebpageURL: info.WebpageURL,
		PreviewURL: previewURL,
		IsLive:     info.IsLive,
		Qualities:  qualities,
		Formats:    formats,
	}
}
