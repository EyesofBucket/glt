# glt

A fast terminal UI for GitLab merge requests and pipelines.

## Features

- Dashboard of review requests, to-dos, your open MRs, and recent projects
- Project page with MRs, pipelines, tags, and project details
- Fuzzy project picker with a list or group tree view
- Create, edit, approve, merge, and rebase MRs, edit labels, and comment
- Pipelines and job logs that update live, with retry, play, and cancel
- Test results from JUnit reports
- Job dependency graph, with each matrix job and each dependency line shown separately
- Themes, mouse support, and drag-to-copy
- Instant startup from a local cache
- Linux, macOS, and Windows

## Quick start

1. Download the binary for your system from the
   [latest release](https://github.com/EyesofBucket/glt/releases/latest),
   rename it to `glt`, and put it on your `PATH`. On Linux and macOS, run
   `chmod +x glt`. With Nix: `nix profile install github:EyesofBucket/glt`.
2. Save a personal access token with the `api` scope:

       glt auth login --host gitlab.example.com

   If glab is already set up, glt imports its hosts and tokens on first
   run and you can skip this step.
3. Run `glt` inside a GitLab repo to open its project page or anywhere else for
   your dashboard. Press `?` for keys.

## Usage

    glt                        project page for the cwd repo, or the dashboard
    glt dash                   dashboard
    glt project                project page
    glt mrs                    merge request list
    glt mr [IID]               an MR (default: the current branch's MR)
    glt mr new                 create an MR from the current branch
    glt ci [PIPELINE_ID]       a pipeline (default: latest on the current branch)
    glt pipelines              pipeline list
    glt tags                   tag list
    glt job JOB_ID             a job log
    glt auth login [--host H]  save a token
    glt auth status            check saved tokens
    glt config                 print the config file path

Flags:

    -R, --repo [HOST/]GROUP/PROJECT   project (default: from the git remote)
    -b, --branch BRANCH               branch (default: the current branch)
        --host HOST                   GitLab instance
        --notify                      notify when a watched pipeline finishes
        --no-mouse                    leave the mouse to the terminal

## Keys

Each view lists its keys at the bottom, and `?` shows them all. These work
everywhere:

| Key | Action |
|---|---|
| `P` | find a project |
| `H` | dashboard |
| `q` / `esc` | back (`q` quits from the start page) |
| `ctrl+c` | quit |
| `ctrl+d` / `ctrl+u` | half page down / up |
| `ctrl+r` | refresh |
| `o` / `y` | open in browser / copy URL |
| `t` | try a theme |
| `,` | settings |

## Configuration

Most options can be changed in the settings popup (`,`), which saves them
to `~/.config/glt/config.yml` (`%AppData%\glt\config.yml` on Windows).
The file is created on first run.

```yaml
default_host: gitlab.com          # used outside a GitLab repo
hosts:
  gitlab.com:
    token: glpat-...              # GITLAB_TOKEN overrides
  gitlab.example.com:
    token: ...
    ssh_host: git.example.com     # if your remotes use an SSH alias
    ca_cert: /path/to/ca.pem      # for self-signed certificates
ui:
  theme: default
  background: true                # false keeps your terminal's background
  picker: list                    # list | tree
  icons: nerd                     # nerd | unicode
  mouse: true
  preview: true                   # log preview in the pipeline view
  notify: false
```

**Themes:** `default` uses your terminal's colours. The others are
`catppuccin-mocha`, `catppuccin-macchiato`, `catppuccin-frappe`,
`catppuccin-latte`, `github-dark`, `github-light`, `gruvbox-dark`,
`gruvbox-light`, `hackerman`, `kanagawa`, `kanagawa-dragon`,
`kanagawa-lotus`, `miasma`, `noctis`, `rose-pine`, `rose-pine-moon`,
`rose-pine-dawn`, `shades-of-purple`, `tokyonight`, `tokyonight-storm`,
`tokyonight-moon` and `tokyonight-day`.

**Icons** need a [Nerd Font](https://www.nerdfonts.com/). Without one, set
`icons: unicode`.

**Editor:** `ctrl+e` in an MR description or comment opens it in the
editor git uses (`GIT_EDITOR`, `core.editor`, `VISUAL`, `EDITOR`, then
`vi`, or Notepad on Windows). GUI editors need their wait flag, such as
`code --wait`.

### Windows

Use Windows Terminal. The old console window lacks the colour, mouse and
clipboard support glt needs. `git` must be on the `PATH` for glt to detect
the repo you're in.

## Build

### With Nix

    nix build                       # ./result/bin/glt
    nix build .#glt-windows         # ./result/bin/glt.exe (x86-64)
    nix build .#glt-windows-arm64   # ./result/bin/glt.exe (ARM)
    nix develop                     # dev shell

### With Go 1.26 or later

    go build ./cmd/glt
    GOOS=windows GOARCH=amd64 go build ./cmd/glt

After changing Go dependencies, update `vendorHash` in `flake.nix`: set it
to `pkgs.lib.fakeHash`, run `nix build`, and copy the hash from the
`got:` line.

## AI Disclosure

With the exception of small adjustments and this disclosure section, this
project is written using AI. I'm currently experimenting with AI's ability to
write code, but also with my own ethical boundaries in regard to my usage of
AI, both for work and personal ventures. If the extent to which AI is used in
this project is outside what you are comfortable with, not only do I
understand, but I may even agree with you.

I try my best to guide agents towards a certain standard of quality and best
practice, and I only release this to the public because I find value in it and
want to make it available for those who might also find it useful.

I see AI as a tool. If a carpenter uses a machine to smooth a piece of lumber,
they should not boast about how smooth they got the board. "I just used the
machine.", they should say, but if the board isn't smooth, they should not
blame the machine, but themselves for not properly checking the machine's work.
I take the same approach to the authorship of this project.

Feel free to submit any issues without worry of me blaming the tools I use.
