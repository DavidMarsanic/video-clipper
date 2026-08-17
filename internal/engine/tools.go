package engine

import (
	"fmt"
	"os/exec"
)

// findYtDlp and findFFmpeg resolve the two external tools this whole
// package delegates to. Neither is bundled — both are expected on PATH,
// per the brief: no auto-downloaded/executed third-party binaries.
func findYtDlp() (string, error) {
	if path, err := exec.LookPath("yt-dlp"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("%w: yt-dlp — install it (macOS: brew install yt-dlp; "+
		"otherwise see https://github.com/yt-dlp/yt-dlp#installation)", ErrMissingDependency)
}

func findFFmpeg() (string, error) {
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("%w: ffmpeg — install it (macOS: brew install ffmpeg; "+
		"otherwise see https://ffmpeg.org/download.html)", ErrMissingDependency)
}
