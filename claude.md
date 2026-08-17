Drop-in CLAUDE.md
Build one thing, well, for Securexe
A self-contained prompt for Claude — paste it as the first message in a new project, or save it as CLAUDE.md at the repo root. It encodes the real build-system constraints, not just preferences, so Claude gets them right the first time instead of discovering them after a failed build.

How to use it
Copy the block below.
Save it as CLAUDE.md in a new repo, or paste it as your first message to Claude / Claude Code.
Describe the one thing your tool should do — Claude has the constraints, you supply the idea.
CLAUDE.md
Copy
# Building a Securexe applet

You are building a small, standalone command-line tool that will be built
and distributed by Securexe (a build-and-distribution platform for Go/Rust
CLI tools). Follow this brief exactly — it encodes real constraints from
the build system, not preferences.

## The one rule: minimal, functional, one thing, seamless

- Solve exactly one problem. Not a multi-tool with a dozen subcommands —
  one job, done completely. If you're tempted to add a second unrelated
  feature, that's a second app.
- No setup ceremony. No config file to hand-edit before first run, no init
  wizard, no "sign in first." Sensible defaults; the tool does real work
  on the very first invocation.
- Standard library first. Every third-party dependency is a cross-compile
  risk (see below) and a maintenance liability. Reach for one only when
  the standard library genuinely can't do the job — and say why in a
  comment when you do.
- Cross-platform from line one. This will be built for linux, macOS, and
  Windows from the same source, automatically. Don't write a Unix-only
  path assumption and fix it later — write it right the first time.

## Hard technical requirements (the build breaks without these)

Pick Go or Rust:

**Go**
- `go.mod` at the repo root.
- One `main` package. If the repo has more than one, name the one you
  want shipped `cmd/<repo-name>` — that's the convention the builder
  looks for.
- CGO_ENABLED=0 at build time — pure Go only. A cgo-dependent dependency
  breaks cross-compilation silently. Before adding a dependency, check it
  doesn't require cgo.
- Don't rely on git-embedded build info (`-buildvcs=false` is set) — if
  you want a version string, hardcode a const or accept it via `-ldflags`.

**Rust**
- `Cargo.toml` with a real binary target: `src/main.rs`, `src/bin/*.rs`,
  an explicit `[[bin]]`, or a workspace member with one.

## Seamless means, concretely

- `--help` (or bare invocation) explains the tool in one screen. No
  hunting through a README to understand what it does.
- Every option available as a flag or env var — interactive prompts are
  a fallback for a human at a terminal, never a requirement for
  automation.
- Real exit codes. 0 for success, non-zero and specific for failure
  modes that matter.

## Optional: a real double-clickable macOS app

A raw binary just dumps CLI usage into Terminal when double-clicked. If
this tool should be double-click-launchable on macOS, add:

    packaging/macos/<app-name>.app/
      Contents/Info.plist
      Contents/MacOS/launch.sh   # execs "<app-name>-bin" next to itself

The builder detects this directory automatically, stages your built
binary in as `<app-name>-bin`, and ships the bundle zipped. Skip this
entirely if the tool is meant to be run from a terminal — that's the
default and it's fine.

## Before you consider it done

- [ ] Solves exactly one problem, stated in one sentence in the README
- [ ] Zero non-stdlib dependencies, or each one justified in a comment
- [ ] Runs correctly with no config file and no first-run setup
- [ ] `--help` alone is enough to use it correctly
- [ ] No cgo — confirm by checking `go build` succeeds with
      `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...` locally
      before calling it done
Once it builds, it's picked up by Securexe automatically — no submission step. Sign in with GitHub to claim a repo you own; featuring after that is manual curation, not automatic.

securexe.worker · applet brief v1