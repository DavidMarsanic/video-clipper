package server

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxPreviewChunk bounds every upstream range request the preview proxy
// makes. CDNs serving signed video URLs (YouTube's googlevideo included)
// commonly reject open-ended or suffix range requests as an anti-leech
// measure — a real player never asks for "the rest of the file" in one
// shot — but happily serve any reasonably-sized bounded range. Translating
// whatever the browser's <video> tag asks for into an always-bounded
// upstream request sidesteps that, so preview works the same for a
// 3-minute clip and a 3-hour one.
const maxPreviewChunk = 8 * 1024 * 1024 // 8 MiB

var previewClient = &http.Client{Timeout: 20 * time.Second}

// handlePreview proxies a single upstream media URL (the direct CDN URL
// Inspect resolved) same-origin, so the browser <video> element never talks
// to the CDN directly. Only used for the preview scrubber — actual
// downloads/clips go straight through yt-dlp/ffmpeg, not this proxy.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(r.URL.Query().Get("src"))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		http.Error(w, "invalid src", http.StatusBadRequest)
		return
	}
	if isPrivateHost(target.Hostname()) {
		http.Error(w, "forbidden target", http.StatusForbidden)
		return
	}

	start, end := int64(0), int64(-1)
	if rng := r.Header.Get("Range"); rng != "" {
		start, end = parseRange(rng)
	}
	if end < 0 || end-start+1 > maxPreviewChunk {
		end = start + maxPreviewChunk - 1
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusBadGateway)
		return
	}
	req.Header.Set("Range", "bytes="+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10))

	resp, err := previewClient.Do(req)
	if err != nil {
		http.Error(w, "upstream fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		w.WriteHeader(resp.StatusCode)
		return
	}
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// parseRange reads a "bytes=start-end" Range header. A missing end (open
// range) or a suffix range ("bytes=-500", which needs the total size to
// resolve and would cost a round trip we don't want) both come back as an
// open end — handlePreview clamps that to maxPreviewChunk either way.
func parseRange(header string) (start, end int64) {
	spec := strings.TrimPrefix(header, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 || parts[0] == "" {
		return 0, -1
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, -1
	}
	if parts[1] == "" {
		return start, -1
	}
	end, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return start, -1
	}
	return start, end
}

// isPrivateHost keeps this proxy from being usable to reach other services
// on the local machine/network — it's meant for one thing, relaying a CDN
// URL yt-dlp just resolved, not for fetching arbitrary hosts.
func isPrivateHost(host string) bool {
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ips, err := net.LookupHost(host)
	if err != nil {
		return true // fail closed
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return true
		}
	}
	return false
}
