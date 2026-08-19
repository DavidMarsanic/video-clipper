// Package server exposes the video-clipper engine over a small JSON+SSE
// HTTP API, bound to loopback only, for the embedded browser-based UI.
package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/DavidMarsanic/video-clipper/engine"
	"github.com/DavidMarsanic/video-clipper/internal/jobs"
	"github.com/DavidMarsanic/video-clipper/web"
)

// idleTimeout is the only auto-shutdown mechanism: if nothing has hit the
// server in this long AND no job is actively running, the process exits.
// There's deliberately no tab-close/pagehide-triggered shutdown — that
// event also fires on ordinary reloads and background-tab suspension, so
// using it to kill the server risks cutting off a session (or an
// in-progress download) that's still very much in use. A double-clicked
// app or a bare `video-clipper` invocation is expected to just keep
// running until Ctrl+C, the idle timeout, or the user quits it.
const idleTimeout = 30 * time.Minute

type Server struct {
	Engine           *engine.Engine
	Jobs             *jobs.Registry
	DefaultOutputDir string

	// ctx is the process's lifetime context (canceled on SIGINT/SIGTERM).
	// Jobs are parented to it so a Ctrl+C kills any in-flight yt-dlp/ffmpeg
	// subprocess instead of orphaning it.
	ctx context.Context

	lastActivity atomic.Int64
}

func New(ctx context.Context, eng *engine.Engine, defaultOutputDir string) *Server {
	s := &Server{
		ctx:              ctx,
		Engine:           eng,
		Jobs:             jobs.NewRegistry(),
		DefaultOutputDir: defaultOutputDir,
	}
	s.touch()
	return s
}

// Start binds 127.0.0.1:port (port 0 picks any free port — this UI is
// never exposed beyond loopback) and serves until the process exits.
func (s *Server) Start(port int) (string, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", fmt.Errorf("starting local server: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/context", s.handleContext)
	mux.HandleFunc("POST /api/inspect", s.handleInspect)
	mux.HandleFunc("POST /api/jobs", s.handleCreateJob)
	mux.HandleFunc("GET /api/jobs/{id}/events", s.handleJobEvents)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.handleJobCancel)
	mux.HandleFunc("POST /api/reveal", s.handleReveal)
	mux.HandleFunc("POST /api/open", s.handleOpen)
	mux.HandleFunc("GET /api/preview", s.handlePreview)
	mux.Handle("GET /", http.FileServer(http.FS(web.Static)))

	httpSrv := &http.Server{Handler: s.trackActivity(mux)}
	go func() {
		_ = httpSrv.Serve(ln)
	}()
	go s.watchIdle()

	return "http://" + ln.Addr().String(), nil
}

func (s *Server) trackActivity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.touch()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) touch() {
	s.lastActivity.Store(time.Now().Unix())
}

func (s *Server) watchIdle() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		idleFor := time.Now().Unix() - s.lastActivity.Load()
		if idleFor > int64(idleTimeout.Seconds()) && !s.Jobs.HasActive() {
			os.Exit(0)
		}
	}
}
