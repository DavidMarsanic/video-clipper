package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DavidMarsanic/video-clipper/internal/browser"
	"github.com/DavidMarsanic/video-clipper/internal/engine"
	"github.com/DavidMarsanic/video-clipper/internal/jobs"
)

func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, DetectContext())
}

func (s *Server) handleInspect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required", "code": "bad-request"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	info, err := s.Engine.Inspect(ctx, req.URL)
	if err != nil {
		errorResponse(w, err)
		return
	}
	// Route the preview through our own proxy rather than handing the
	// browser a direct CDN URL: googlevideo (and similar) CDNs reject the
	// open-ended range requests a <video> tag makes against large files,
	// which handlePreview works around by always requesting bounded
	// chunks upstream.
	if info.PreviewURL != "" {
		info.PreviewURL = "/api/preview?src=" + url.QueryEscape(info.PreviewURL)
	}
	writeJSON(w, http.StatusOK, info)
}

type createJobRequest struct {
	URL       string   `json:"url"`
	Mode      string   `json:"mode"` // "full" | "clip"
	Start     *float64 `json:"start"`
	End       *float64 `json:"end"`
	Format    string   `json:"format"`  // "auto" | "mp4" | "audio"
	Quality   string   `json:"quality"` // "best" | "1080p" | ...
	OutputDir string   `json:"outputDir"`
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req createJobRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required", "code": "bad-request"})
		return
	}

	outDir := req.OutputDir
	if outDir == "" {
		outDir = s.DefaultOutputDir
	}
	opts := engine.Options{Format: req.Format, Quality: req.Quality, OutputDir: outDir}

	var start, end *time.Duration
	if req.Start != nil {
		d := time.Duration(*req.Start * float64(time.Second))
		start = &d
	}
	if req.End != nil {
		d := time.Duration(*req.End * float64(time.Second))
		end = &d
	}

	job, ctx := s.Jobs.Create(s.ctx)

	go func() {
		onProgress := func(p engine.Progress) {
			job.Publish(jobs.Event{Stage: p.Stage, Percent: p.Percent, Speed: p.Speed, ETA: p.ETA, Message: p.Message})
		}

		var result *engine.Result
		var err error
		switch {
		case req.Format == "audio":
			result, err = s.Engine.ExtractAudio(ctx, req.URL, start, end, opts, onProgress)
		case req.Mode == "clip" && start != nil && end != nil:
			result, err = s.Engine.Clip(ctx, req.URL, *start, *end, opts, onProgress)
		default:
			result, err = s.Engine.Download(ctx, req.URL, opts, onProgress)
		}

		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				job.Publish(jobs.Event{Stage: "canceled"})
				return
			}
			code, msg := classifyCode(err)
			job.Publish(jobs.Event{Stage: "error", Message: msg, Code: code})
			return
		}
		job.Publish(jobs.Event{Stage: "done", Percent: 100, Path: result.Path, Filename: result.Filename})
	}()

	writeJSON(w, http.StatusOK, map[string]string{"jobId": job.ID})
}

func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	job, ok := s.Jobs.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, cancel := job.Subscribe()
	defer cancel()

	for {
		select {
		case e, open := <-ch:
			if !open {
				return
			}
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if e.Stage == "done" || e.Stage == "error" || e.Stage == "canceled" {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	job, ok := s.Jobs.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	job.Cancel()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := browser.Reveal(req.Path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := browser.Open(req.Path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers -----------------------------------------------------------

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body", "code": "bad-request"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func classifyCode(err error) (code, message string) {
	switch {
	case errors.Is(err, engine.ErrUnsupportedURL):
		return "unsupported", err.Error()
	case errors.Is(err, engine.ErrDRMProtected):
		return "drm", err.Error()
	case errors.Is(err, engine.ErrAuthRequired):
		return "auth", err.Error()
	case errors.Is(err, engine.ErrFormatUnavailable):
		return "format", err.Error()
	case errors.Is(err, engine.ErrMissingDependency):
		return "missing-tool", err.Error()
	default:
		return "error", err.Error()
	}
}

func httpStatusFor(code string) int {
	switch code {
	case "unsupported", "drm", "auth", "format":
		return http.StatusUnprocessableEntity
	case "missing-tool":
		return http.StatusFailedDependency
	case "bad-request":
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func errorResponse(w http.ResponseWriter, err error) {
	code, msg := classifyCode(err)
	writeJSON(w, httpStatusFor(code), map[string]string{"error": msg, "code": code})
}
