// Package engine wraps yt-dlp and ffmpeg behind a small, UI-agnostic
// interface: Inspect, Download, Clip, ExtractAudio. It's a public
// (non-internal) package on purpose — clip-and-gif imports it directly as
// a real Go module dependency to fetch/clip a source URL, rather than
// duplicating this logic.
package engine

import "errors"

// VideoInfo is the metadata returned by Inspect.
type VideoInfo struct {
	Title      string       `json:"title"`
	Duration   float64      `json:"duration"` // seconds; 0 if unknown (e.g. live)
	Thumbnail  string       `json:"thumbnail"`
	Uploader   string       `json:"uploader"`
	Site       string       `json:"site"`
	WebpageURL string       `json:"webpageUrl"`
	PreviewURL string       `json:"previewUrl,omitempty"`
	IsLive     bool         `json:"isLive"`
	Qualities  []string     `json:"qualities"` // e.g. ["best","1080p","720p"], only what's actually available
	Formats    []FormatInfo `json:"formats"`
}

// FormatInfo is a simplified view of one yt-dlp format entry.
type FormatInfo struct {
	FormatID   string `json:"formatId"`
	Ext        string `json:"ext"`
	Resolution string `json:"resolution"`
	VCodec     string `json:"vcodec"`
	ACodec     string `json:"acodec"`
	Note       string `json:"note"`
	Filesize   int64  `json:"filesize"`
}

// Options carries the user's format/quality/output choices. Range (for
// Clip/ExtractAudio) is passed separately as start/end parameters.
type Options struct {
	Format    string // "auto" | "mp4" | "audio"
	Quality   string // "best" | "1080p" | "720p" | "480p" | "360p"
	OutputDir string
}

// Progress is streamed to the caller-supplied callback during Download/Clip/ExtractAudio.
type Progress struct {
	Stage   string  `json:"stage"` // resolving, downloading, trimming, done, error
	Percent float64 `json:"percent"`
	Speed   string  `json:"speed,omitempty"`
	ETA     string  `json:"eta,omitempty"`
	Message string  `json:"message,omitempty"`
}

// Result describes the file written to disk.
type Result struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
}

// Sentinel errors, classified from yt-dlp's own error output so callers can
// show one honest line instead of a raw subprocess dump. Use errors.Is to
// check these against an error returned from this package.
var (
	ErrUnsupportedURL    = errors.New("unsupported URL")
	ErrDRMProtected      = errors.New("DRM-protected media")
	ErrAuthRequired      = errors.New("sign-in or authentication required")
	ErrFormatUnavailable = errors.New("requested format unavailable")
	ErrMissingDependency = errors.New("required tool not found")
)
