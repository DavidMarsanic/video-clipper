package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// findYtDlp and findFFmpeg resolve the two external tools this whole
// package delegates to. Neither is bundled — both are expected on PATH,
// per the brief: no auto-downloaded/executed third-party binaries.
func findYtDlp() (string, error) {
	if path, err := lookPath("yt-dlp"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("%w: yt-dlp — install it (macOS: brew install yt-dlp; "+
		"otherwise see https://github.com/yt-dlp/yt-dlp#installation)", ErrMissingDependency)
}

func findFFmpeg() (string, error) {
	if path, err := lookPath("ffmpeg"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("%w: ffmpeg — install it (macOS: brew install ffmpeg; "+
		"otherwise see https://ffmpeg.org/download.html)", ErrMissingDependency)
}

// lookPath resolves name via the standard PATH lookup, falling back to a
// short list of common install locations for the current OS if that fails.
//
// This exists because a GUI-launched process on macOS — whether opened via
// Finder/LaunchServices or spawned by securexe-launcher — does not inherit
// the user's interactive shell PATH. It gets the OS's minimal default
// (typically /usr/bin:/bin:/usr/sbin:/sbin), which excludes Homebrew's
// install directories entirely. A terminal-launched build never hits this,
// because the shell already sourced .zprofile/.zshrc and put Homebrew on
// PATH — which is exactly why this bug can pass local testing and then fail
// for a real double-clicked build. See also gif-maker/engine/engine.go,
// which has the same fallback for the same reason — keep the two in sync.
func lookPath(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	candidateName := name
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		candidateName = name + ".exe"
	}
	for _, dir := range fallbackToolDirsFunc() {
		candidate := filepath.Join(dir, candidateName)
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// fallbackToolDirsFunc is a var (not a plain func call) so tests can
// substitute a temp directory instead of the real fallback locations.
var fallbackToolDirsFunc = fallbackToolDirs

// fallbackToolDirs lists common install locations for CLI tools that a
// GUI-launched process's minimal PATH won't include, ordered by likelihood.
func fallbackToolDirs() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/opt/homebrew/bin", // Homebrew on Apple Silicon
			"/usr/local/bin",    // Homebrew on Intel Macs
			"/opt/local/bin",    // MacPorts
		}
	case "linux":
		return []string{
			"/usr/local/bin",
			"/snap/bin",
			"/var/lib/flatpak/exports/bin",
		}
	case "windows":
		dirs := []string{`C:\ProgramData\chocolatey\bin`}
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, "scoop", "shims"))
		}
		return dirs
	default:
		return nil
	}
}

// isExecutableFile reports whether path is a regular file that can be run
// as a command. Windows has no executable bit to check, so existence as a
// non-directory is treated as sufficient there.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0111 != 0
}

// expectedYtDlpVersion and expectedFFmpegVersion are what this engine was
// actually last tested against — keep in sync with the "version" field in
// brightencode.json's matching dependency entry by hand; nothing enforces the
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
