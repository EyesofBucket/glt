# glt

A fast terminal UI for GitLab merge requests and pipelines.

## Why it's fast

- **Draws from cache straight away.** Every view draws its last known data at
  once, from memory or from a disk cache (`~/.cache/glt/<host>`), then
  refreshes in the background. A repeat launch shows a full pipeline in about
  250ms.
- **Few round trips.** The MR list is a single GraphQL query that includes each
  MR's pipeline status. Pipeline jobs and trigger jobs are fetched in parallel.
  Connections are reused (HTTP/2 keep-alive).
- **Prefetch.** When the cursor rests on an MR or pipeline, glt loads its
  details so opening it feels instant.
- **Incremental logs.** Job logs are fetched with HTTP `Range` (only the new
  bytes), and only the lines on screen are drawn, so very long logs stay smooth.
- **Polite polling.** Only the visible view refreshes: about every 3s while a
  pipeline is running, and much less often once it has finished. When GitLab
  returns 429, glt backs off for every request at once.

## Usage

    glt                     the cwd repo's home page, or the dashboard when
                            the cwd isn't a GitLab repo
    glt dash                the dashboard
    glt project             the project's home page
    glt mrs                 merge request list
    glt mr [IID]            an MR (default: the current branch's MR)
    glt mr new              create an MR from the current branch
    glt ci [PIPELINE_ID]    a pipeline (default: latest on the current branch;
                            follows new pipelines as you push)
    glt pipelines           pipeline list
    glt tags                tag list
    glt job JOB_ID          a job log
    glt -R group/project …  pick the project explicitly (or host/group/project)
    glt auth login [--host H]   save a token (prompts; verified before saving)
    glt auth status             check every saved token
    glt config                  print the config path

**Dashboard** (`H` from anywhere): review requests, your to-dos, your open MRs,
and recently visited projects, across all projects on the host.

**Project home page**: details, merge requests, pipelines and tags as four
summary panes. Select a pane with `tab`, `1-4`, the arrow keys or a click.
`enter` (or a double-click) opens the full MR list, pipeline list or tag
list. The items inside the panes are a summary and can't be selected.
Opening a project from the picker or the dashboard lands here, and `esc`
from its pages comes back here.

**Project picker** (`P` from anywhere): a Telescope-style fuzzy finder over
your projects. It also searches the server for projects you aren't a member of,
and shows a preview with the default branch, open MR count and latest pipeline.
`enter` opens the project's home page. `ctrl+t` switches to a tree of your
groups, collapsed to start with. In the tree, `enter` on a group expands or
collapses it, and `enter` on a project opens it. `←`/`→` also collapse and
expand. Typing filters the tree and opens the groups that have matches. The
picker remembers which view you used last during a session.

## Config

`~/.config/glt/config.yml` is created on first run (honours `$XDG_CONFIG_HOME`
and `$GLT_CONFIG_DIR`). If glab is set up, its hosts and tokens are imported
once; after that glt never reads glab's config.

```yaml
default_host: gitlab.com     # used when the cwd isn't a repo on a known host
hosts:
  gitlab.com:
    token: glpat-…           # "api" scope; GITLAB_TOKEN overrides
  gitlab.example.com:
    token: …
    ssh_host: git.example.com   # if remotes use an SSH alias
    ca_cert: /path/to/ca.pem    # for self-signed instances
ui:
  icons: nerd                # nerd | unicode
  theme: default             # see Themes below
  background: true           # false: keep the terminal's background under a theme
  picker: list               # project picker view: list | tree
  mouse: true               # click, scroll and drag-to-copy
  preview: true              # log preview pane in the pipeline view
  notify: false              # desktop notification when a watched pipeline finishes
```

Visited projects are stored in `~/.local/state/glt/<host>/recent.json`, and
API responses are cached in `~/.cache/glt/<host>/`.

## Keys

Press `?` in any view. The main ones:

| View | Keys |
|---|---|
| everywhere | `P` find project, `H` dashboard, `j/k` move, `g/G` top/bottom, `ctrl+d/u` half page, `q`/`esc` back (`q` quits from the dashboard, or from the home page of the project you started in), `ctrl+c` quit, `ctrl+r` refresh, `t` try a theme, `,` settings, `o` open in browser, `y` copy URL, `/` filter |
| project | `tab`/`1-4`/`h j k l` select pane, `enter` open its page, `c` new MR, `o` browser, `y` copy URL |
| tags | `enter` the tag's pipelines, `/` search, `o` browser, `y` copy name |
| dashboard | `tab`/`1-4`/`h l` switch pane, `enter` open, `p` MR pipeline, `x` mark to-do done, `X` mark all done |
| label editor | type to filter, `ctrl+d/u` half page, `tab` or click to toggle, `enter` save, `esc` cancel |
| picker | type to filter, `ctrl+n/p` move, `ctrl+d/u` half page, `enter` open (in the tree, expands a group), `ctrl+t` tree/list view, `←/→` collapse/expand, `ctrl+o` browser, `ctrl+y` copy URL, `esc` close |
| MR list | `enter` open, `p` pipeline, `tab`/`1-5` all open/mine/review requested/merged/closed, `c` new MR, `B` your branch's pipelines |
| new/edit MR | `j/k`/`tab` move between fields, `enter` type in the title or description (`esc` stops typing) or choose branch/template/labels/people or toggle, `ctrl+e` write the description in your editor, `ctrl+s` create/save, `esc`/`q` cancel |
| MR | `p` pipeline, `T` test results, `e` edit (target, title, description, labels, assignees, reviewers, draft, merge options), `n` run a new pipeline, `l` edit labels, `a` approve/unapprove, `m` merge, `M` auto-merge, `R` rebase, `d` toggle draft, `c` comment (`ctrl+e` writes it in your editor) |
| pipeline list | `enter` open, `b` pick a branch (your checked-out one first, or all branches), `n` run a pipeline, `R`/`X` retry/cancel, `/` search |
| pipeline | `enter` log (or downstream pipeline), `r` retry job, `p` play manual job, `x` cancel job, `R`/`X` retry/cancel pipeline, `]/[` next/prev failed job, `v` toggle log preview, `T` test results |
| test results | `f` failed/all/skipped, `/` search, `enter` fold a suite or read a test's output, `tab` switch to the output, `]/[` next/prev failure, `z` fold all, `J` the suite's job log, `y` copy output, `o` the Tests tab in the browser, `esc` close |
| log | `f` follow, `w` wrap, `T` timestamps, `#` line numbers, `/` `n` `N` search, `[ ]` jump between sections, `h/l` scroll sideways, `r/p/x` retry/play/cancel |

### Writing in your editor

`ctrl+e` in an MR description or a comment opens the text in your editor
and puts what you save back into the field to send. glt uses the editor
git uses for commit messages: `GIT_EDITOR`, `core.editor`, `VISUAL`, then
`EDITOR`, falling back to `vi` (Notepad on Windows). Editors that open a
window need their wait flag, e.g. `code --wait`, and Notepad++ needs
`-multiInst -nosession`; Git for Windows' installer sets these up when you
pick an editor. On Windows, quote paths that contain spaces.

### Test results

When the MR's pipeline publishes JUnit reports (`artifacts:reports:junit`),
the MR page shows a Tests line under the pipeline with the counts. Press
`T` there, or on a pipeline page, to open the report in a floating pane:
suites and their tests on the left, the highlighted test's details, output
and stack trace on the right. It opens on the failures, or on every test
when nothing failed.

These are the results of the MR's latest pipeline. GitLab's "new failures
compared to the target branch" view isn't available through the API.

### Status line

The line above the key hints is laid out like lualine:

    [ MODE ][ project ]                                   [ 3/11 ][ you@host ]

- **Mode** shows what has the keyboard: blue while browsing, green while
  typing or in a picker, magenta while dragging a selection, yellow while
  confirming.
- **Position** is `row/total` in lists, and Top/Bot/All or a percentage in
  the MR page and job logs.
- Your user and host are always on the right. A spinner or a rate-limit
  warning appears just left of the position.

### Settings and themes

Press `,` for settings: the default GitLab instance, the default theme,
whether themes paint their background, icons, mouse, log preview and
notifications. Changes are written straight
to the config file, keeping its comments, and take effect immediately.
Changing the default instance offers to switch to it now.

Press `t` to try a theme. Each theme previews as you move through the list.
`enter` keeps it for this session and `esc` puts the old one back. To make
a theme the default, set it in settings or with `ui.theme` in the config.

The `default` theme uses your terminal's 16 colours, so it works
everywhere and follows your terminal's theme. The other themes use their
published true-colour palettes and paint their own background, unless you
turn "Theme background" off (`ui.background: false`) to keep your
terminal's background and transparency. They also
map ANSI colours in job logs onto the theme.

- `catppuccin-mocha`, `catppuccin-macchiato`, `catppuccin-frappe`, `catppuccin-latte`
- `github-dark`, `github-light`
- `gruvbox-dark`, `gruvbox-light`
- `hackerman` (Omarchy)
- `miasma`
- `kanagawa` (Wave), `kanagawa-dragon`, `kanagawa-lotus`
- `noctis`
- `rose-pine`, `rose-pine-moon`, `rose-pine-dawn`
- `shades-of-purple`
- `tokyonight`, `tokyonight-storm`, `tokyonight-moon`, `tokyonight-day`

### Mouse

Click a row to select it and double-click to open it. Tabs, panes, the
pipeline line on an MR and the steps in the status line can be clicked too. The
wheel scrolls whatever is under the pointer.

Drag to select text. The selection stays inside the pane you started in, and
lines wrap at that pane's edges instead of running into the next one.
Releasing the button copies the text and clears the selection. Hold shift
for your terminal's own selection.

## Build

    nix run .               # run
    nix build               # ./result/bin/glt
    nix profile install .   # install
    nix develop             # dev shell with go, gopls, golangci-lint, delve
    nix build .#glt-windows        # ./result/bin/glt.exe (x86-64)
    nix build .#glt-windows-arm64  # ./result/bin/glt.exe (ARM)

Without Nix, `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/glt`
also works; there's no C code to cross-compile.

### Windows

- Use Windows Terminal. The old console window has no true colour, mouse
  or clipboard escape support. Install a Nerd Font, or set
  `ui.icons: unicode`.
- Config lives in `%AppData%\glt\config.yml`, and the cache and
  recent-projects state in `%LocalAppData%\glt`. On first run glt imports
  glab's hosts from `%USERPROFILE%\.config\glab-cli` or
  `%AppData%\glab-cli`.
- `o` opens links in the default browser, and pipeline notifications
  (`--notify`) show as Windows toasts through PowerShell.
- Copying uses the terminal's clipboard escape (OSC 52), which Windows
  Terminal supports.
- `git` must be on the `PATH` for glt to detect the repo you're in.

After changing dependencies, update `vendorHash` in `flake.nix`. Set it to
`pkgs.lib.fakeHash`, run `nix build`, and copy the hash from the "got:" line.

## Releasing

CI tests every branch push and pull request. To make a release, tag the
commit with a version and push the tag:

```sh
git tag v0.2.0
git push origin v0.2.0
```

The release workflow tests that commit. GoReleaser (`.goreleaser.yaml`)
then builds archives for Linux, macOS and Windows on amd64 and arm64, and
publishes them to a GitHub release with checksums and generated notes.
Tags with a suffix, such as `v0.2.0-rc.1`, are marked as pre-releases.
To try the build locally from the dev shell, run
`goreleaser release --snapshot --clean` (output goes in `dist/`).
