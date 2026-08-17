package server

import (
	"encoding/json"
	"net/url"
	"os"
)

// Context is Securexe's current-context information, when available:
// the active browser tab's URL, frontmost app, and window title.
type Context struct {
	URL          string `json:"url,omitempty"`
	FrontmostApp string `json:"frontmostApp,omitempty"`
	WindowTitle  string `json:"windowTitle,omitempty"`
	Supported    bool   `json:"supported"`
}

// DetectContext is a best-effort adapter: no concrete Securexe context API
// is documented anywhere available to this tool, so it checks a small set
// of plausible environment-variable conventions and otherwise degrades to
// an empty context — the UI just falls back to focusing the URL input.
// This is intentionally isolated to one file so it's a one-place change
// once Securexe's real context API is known.
func DetectContext() Context {
	var c Context

	if raw := os.Getenv("SECUREXE_CONTEXT_JSON"); raw != "" {
		var parsed struct {
			URL          string `json:"url"`
			FrontmostApp string `json:"frontmostApp"`
			WindowTitle  string `json:"windowTitle"`
		}
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			c.URL, c.FrontmostApp, c.WindowTitle = parsed.URL, parsed.FrontmostApp, parsed.WindowTitle
		}
	}
	if c.URL == "" {
		c.URL = firstNonEmpty(os.Getenv("SECUREXE_CONTEXT_URL"), os.Getenv("SECUREXE_URL"))
	}
	if c.FrontmostApp == "" {
		c.FrontmostApp = os.Getenv("SECUREXE_FRONTMOST_APP")
	}
	if c.WindowTitle == "" {
		c.WindowTitle = os.Getenv("SECUREXE_WINDOW_TITLE")
	}

	c.Supported = LooksLikeURL(c.URL)
	return c
}

// LooksLikeURL is a cheap, generic heuristic used both for the Securexe
// context and for deciding when to auto-inspect a pasted/typed value —
// real site support is determined later by yt-dlp itself at Inspect time.
func LooksLikeURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
