package engine

import (
	"fmt"
	"os/exec"
	"strings"
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

// expectedYtDlpVersion and expectedFFmpegVersion are what this engine was
// actually last tested against — keep in sync with the "version" field in
// securexe.json's matching dependency entry by hand; nothing enforces the
// two staying equal, but a mismatch between them here is a real bug.
const (
	expectedYtDlpVersion  = "2026.07.04"
	expectedFFmpegVersion = "9.0.1"
)

// checkToolVersions runs `--version` against each resolved tool and
// returns one human-readable note per tool whose reported version doesn't
// match what this engine was last tested against. This is deliberately
// not a hard failure — a newer or older tool very often still works fine
// — just a way to make version drift visible (in logs, and via whatever
// the caller does with the notes) instead of silently invisible, since
// nothing here pins or auto-updates either tool.
func checkToolVersions(ytDlpPath, ffmpegPath string) []string {
	var notes []string
	if v, err := toolVersion(ytDlpPath, "--version"); err == nil && v != expectedYtDlpVersion {
		notes = append(notes, fmt.Sprintf(
			"yt-dlp is %s, this app was last tested against %s — should still work, but if something breaks, that's the first thing to check",
			v, expectedYtDlpVersion))
	}
	if v, err := ffmpegVersion(ffmpegPath); err == nil && v != expectedFFmpegVersion {
		notes = append(notes, fmt.Sprintf(
			"ffmpeg is %s, this app was last tested against %s — should still work, but if something breaks, that's the first thing to check",
			v, expectedFFmpegVersion))
	}
	return notes
}

func toolVersion(path string, args ...string) (string, error) {
	out, err := exec.Command(path, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ffmpeg's --version output starts with "ffmpeg version 9.0.1 Copyright...";
// only the version token itself is worth comparing.
func ffmpegVersion(path string) (string, error) {
	out, err := toolVersion(path, "-version")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	for i, f := range fields {
		if f == "version" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("unrecognized ffmpeg -version output")
}
