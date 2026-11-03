# glt

A terminal UI for GitLab merge requests and pipelines, written in Go with
Bubble Tea. The binary is `glt`, the Go module is `gitlab-tui`, and the
repo is github.com/EyesofBucket/glt. `README.md` is the user guide; this
file is for people and agents changing the code.

## Setup

You need Go (the version in `go.mod`) and git. The repo has a Nix flake
that provides both, plus goreleaser:

    nix develop                      # shell with the toolchain
    nix develop -c go test ./...     # or one command at a time

Without Nix, install Go yourself and run the same `go` commands.

To run glt against a GitLab instance you need a personal access token with
the `api` scope. Save it with `glt auth login --host <host>` or set
`GITLAB_TOKEN`. Config is in `~/.config/glt/config.yml`
(`%AppData%\glt\config.yml` on Windows; `glt config` prints the path).
The API cache is in `glt/<host>` under the OS cache dir, and deleting it is
always safe.

## Build, test, check

    go build -o /tmp/glt ./cmd/glt
    go test ./...
    gofmt -l .        # must print nothing
    go vet ./...

CI runs all of these on Linux, macOS and Windows, cross-compiles every
release target, and builds the Nix package. Run them before pushing.

Nix gotchas:
- `nix build` and `nix profile` only see files git tracks. A new file that
  isn't `git add`ed fails the Nix build with "undefined: ...", even though
  `go build` works.
- After changing Go dependencies, update `vendorHash` in `flake.nix`: set
  it to `pkgs.lib.fakeHash`, run `nix build`, and copy the hash from the
  `got:` line.

## Code layout

- `cmd/glt/main.go`: subcommands and flags. It works out the project from
  the git remote and opens the matching view.
- `internal/config`: the config file (hosts, tokens, UI options).
- `internal/gitlab`: a small REST and GraphQL client (`client.go`) and the
  calls glt makes (`api.go`). Put new endpoints in `api.go`. A project
  argument is either a path (`group/project`) or a numeric ID string, and
  calls must accept both.
- `internal/ui`: everything on screen.

### How the UI fits together

- **App** (`app.go`): holds a stack of views, an optional overlay, an
  optional modal (y/n confirm or text prompt), the flash message and the
  footer. Global keys (`P`, `H`, `q`/`esc`, `ctrl+r`, `,`, `t`, `?`) are
  handled here before the view sees them.
- **Views** (pages) implement the `view` interface in `app.go`. Open one
  with `a.push(v)`. `a.back()` pops it, unless the view implements
  `guardLeave(*App) bool` (forms with unsaved changes do). A view whose
  `capturing()` returns true gets raw keys, for text input.
- **Overlays** (floating popups such as the project picker, tests and job
  dependencies) implement `overlay` and are opened with `a.overlay = x`.
  Copy `tests.go` or `deps.go` when adding one. Overlays don't get the
  app's key-burst splitting: a fast key repeat can arrive as one message
  with several runes, so split it as those two do.
- **Data** (`store.go`): a stale-while-revalidate cache. Views never wait
  on the network. `render` shows whatever `get[T](a.store, key)` returns
  (possibly from the previous session's disk cache), and `refresh` returns
  `fetch(a.store, key, maxAge, fn)` commands. Keys look like
  `jobs:<project>:<pipeline>`, with helpers such as `jobsKey`.
- **Actions** that change GitLab go through `a.action(desc, fn, invalidate,
  then)`, which shows progress, reports errors and refetches the
  invalidated keys. Destructive ones ask first with `a.confirm(prompt,
  onYes)`. Free text is `a.prompt(title, onInput)`, and long text can open
  the user's editor with `a.editText` (`editor.go`, Windows included).
- **Mouse** (`mouse.go`): make text clickable or scrollable by wrapping it
  in `a.zone(s, zone{...})` while rendering. Zones are found in the final
  frame, so views don't track screen positions. Layer 0 is pages, 1 is
  overlays, 2 is the confirm popup.
- **Styles** (`styles.go`, `themes.go`, `paint.go`): colours come from the
  active theme. Use the palette variables (`cBlue`, `cGray`, ...) and the
  shared styles (`sDim`, `sActive`, `sDirect`, ...), and never hard-code a
  colour. The styles are rebuilt when the theme changes, so look them up
  when rendering rather than copying them. Shared pieces: `pane` (rounded
  box with a title), `button`, `selectLine`, `statusIcon`/`statusLabel`,
  `fit`/`pad` (ANSI-aware width), and `overlayAt`.
- **Icons**: use the `ic` icon set, never a literal Nerd Font glyph. Users
  without a Nerd Font switch to `icons: unicode`.
- **Job logs** (`trace.go`, `traceview.go`) are fetched incrementally and
  pre-processed once. The `default` theme uses the terminal's own colours,
  and `paint.go` maps the 16 ANSI colours onto true-colour themes.
- **Dependency graph** (`graph.go`, `deps.go`): the layout's doc comments
  explain the routing rules (one forking line per job, carriers through
  skipped columns, one lane per line, crossings vs junctions). Read them
  before changing it. `graph_test.go` logs the layouts as text, so run it
  with `-v` to see them.

## UI conventions

- Look: rounded borders, blue for the active pane, gray for inactive ones,
  a plain breadcrumb header, underlined tabs, and buttons as blocks of
  colour (`button`). Don't add coloured label blocks like a tmux status
  bar; that look was tried and rejected.
- Every view lists its keys in `help()`, shown in the footer and in `?`.
  Keys are vim-style (`hjkl`, `g`/`G`, `ctrl+d`/`ctrl+u`, `/` to search)
  and should mean the same thing everywhere.
- Flash messages are short and lowercase ("no pipeline to show tests
  for").
- It must work on Linux, macOS and Windows (Windows Terminal), with or
  without a mouse and a Nerd Font, and in narrow terminals.

## Trying changes by hand

Build, then run glt in a detached tmux session to drive it with keys and
read the screen:

    tmux new-session -d -s t -x 200 -y 50 "/tmp/glt mr 123 -R gitlab.com/group/project"
    tmux send-keys -t t D
    tmux capture-pane -t t -p      # add -e to see colours

Use a project you have access to and that has the data you need, such as
pipelines with `needs` or JUnit test reports. When the project isn't
yours, only read: don't retry, cancel, merge, approve, comment on or edit
anything unless its owner has agreed.

## Writing

- Code comments: short, and about why rather than what. Match the density
  and tone of the code around you.
- README, UI text, commit messages and PR descriptions: plain and brief.
  Don't use em dashes. The README covers the current state only (no
  history or plans), and it's a user and build guide, not a design
  document.

## Workflow

- Branch off `main` and merge through a pull request. CI runs on pull
  requests and on pushes to `main`.
- Agents: don't commit, push, tag or open PRs unless the person you're
  working with asks you to.
- Releases: pushing a `v*` tag runs CI and then GoReleaser
  (`.goreleaser.yaml`). It publishes plain binaries named
  `glt-<version>-<os>-<arch>` (`.exe` on Windows), plus `README.md` and
  `checksums.txt`, and GitHub writes the notes from the merged PRs. Use a
  minor bump for features and a patch bump for fixes.
- To release: bump `VERSION` in a pull request, merge it, then tag that
  commit `v<VERSION>` and push the tag. The release workflow fails if the
  tag and `VERSION` differ. GoReleaser takes the version from the tag;
  the Nix build reads `VERSION` and adds the commit's date and hash
  (`0.3.0+20261012.fdc6bc4`), because a flake can't see tags.
