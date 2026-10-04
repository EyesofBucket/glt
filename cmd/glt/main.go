// Command glt is a fast terminal UI for GitLab merge requests and pipelines.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"gitlab-tui/internal/config"
	"gitlab-tui/internal/gitlab"
	"gitlab-tui/internal/ui"
)

var version = "dev"

const usage = `glt - fast GitLab MRs and pipelines in your terminal

Usage:
  glt [flags]                 the cwd project's home page, or the
                              dashboard when not in a GitLab repo
  glt dash                    the dashboard
  glt project                 the project's home page
  glt mrs                     merge request list
  glt mr [IID]                an MR (default: the current branch's MR)
  glt mr new                  create an MR from the current branch
  glt ci [PIPELINE_ID]        a pipeline (default: latest on the current
                              branch, following new pipelines as you push)
  glt pipelines               pipeline list (alias: pl)
  glt tags                    tag list
  glt job JOB_ID              a job log
  glt auth login [--host H]   save a personal access token for a host
  glt auth status             check the saved tokens
  glt config                  print the config file path

Flags:
  -R, --repo [HOST/]GROUP/PROJECT   project (default: from git remote)
  -b, --branch BRANCH               branch (default: current branch)
      --host HOST                   GitLab host (default: from remote, else
                                    default_host in the config)
      --notify                      desktop notification when a watched pipeline finishes
      --no-mouse                    don't capture the mouse (no clicks or drag-to-copy)
      --version                     print version

Config: ~/.config/glt/config.yml (created on first run).
Press P anywhere to jump to a project, ? for keys, q/esc to go back (q quits from home),
ctrl+c to quit.
`

func main() {
	args := os.Args[1:]
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet("glt", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	var repo, branch, host string
	var notify, noMouse, showVersion bool
	fs.StringVar(&repo, "R", "", "")
	fs.StringVar(&repo, "repo", "", "")
	fs.StringVar(&branch, "b", "", "")
	fs.StringVar(&branch, "branch", "", "")
	fs.StringVar(&host, "host", "", "")
	fs.BoolVar(&notify, "notify", false, "")
	fs.BoolVar(&noMouse, "no-mouse", false, "")
	fs.BoolVar(&showVersion, "version", false, "")
	// allow flags both before and after positional args
	var pos []string
	for {
		fs.Parse(args)
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if sub == "" && len(pos) > 0 {
		sub, pos = pos[0], pos[1:]
	}
	if showVersion {
		fmt.Println("glt", version)
		return
	}
	if sub == "help" {
		fmt.Print(usage)
		return
	}

	cfg, notice, err := config.Load()
	if err != nil {
		die("config: %v", err)
	}
	if notice != "" {
		fmt.Fprintln(os.Stderr, "glt: "+notice)
	}

	switch sub {
	case "config":
		fmt.Println(config.Path())
		return
	case "auth":
		auth(cfg, pos, host)
		return
	}

	argID := func() int {
		if len(pos) == 0 {
			return 0
		}
		n, err := strconv.Atoi(strings.TrimLeft(pos[0], "!#"))
		if err != nil {
			die("invalid ID %q", pos[0])
		}
		return n
	}

	ctx, err := config.Resolve(cfg, repo, branch, host)
	if err != nil {
		die("%v", err)
	}
	client := gitlab.New(ctx.APIBase, ctx.Token, tlsOpts(ctx))
	needProject := func() {
		if ctx.Project == "" {
			die("not in a GitLab repository; pass -R group/project")
		}
	}
	needBranch := func() {
		if ctx.Branch == "" {
			die("no current branch; pass -b BRANCH or an ID")
		}
	}

	dash := ui.NewDashboard()
	// project pages sit on top of the dashboard and the project's home page,
	// so esc walks back through them
	in := func(vs ...ui.View) []ui.View {
		return append([]ui.View{dash, ui.NewProject(ctx.Project)}, vs...)
	}
	var stack []ui.View
	switch sub {
	case "":
		stack = []ui.View{dash}
		if ctx.Project != "" {
			stack = in()
		}
	case "dash", "dashboard", "home":
		stack = []ui.View{dash}
	case "project", "proj":
		needProject()
		stack = in()
	case "mrs", "list", "ls":
		needProject()
		stack = in(ui.NewMRList(ctx.Project))
	case "mr":
		needProject()
		if len(pos) > 0 && (pos[0] == "new" || pos[0] == "create") {
			stack = in(ui.NewMRList(ctx.Project), ui.NewMRForm(ctx.Project, ctx.Branch))
		} else if id := argID(); id != 0 {
			stack = in(ui.NewMRList(ctx.Project), ui.NewMR(ctx.Project, id))
		} else {
			needBranch()
			stack = in(ui.NewMRList(ctx.Project), ui.NewMRForBranch(ctx.Project, ctx.Branch))
		}
	case "ci", "pipeline", "p":
		needProject()
		if id := argID(); id != 0 {
			stack = in(ui.NewPipelineList(ctx.Project, ""), ui.NewPipeline(ctx.Project, id, ""))
		} else {
			needBranch()
			stack = in(ui.NewPipelineList(ctx.Project, ctx.Branch), ui.NewPipeline(ctx.Project, 0, ctx.Branch))
		}
	case "pipelines", "pl":
		needProject()
		stack = in(ui.NewPipelineList(ctx.Project, ""))
	case "tags":
		needProject()
		stack = in(ui.NewTagList(ctx.Project))
	case "job", "trace", "log":
		needProject()
		id := argID()
		if id == 0 {
			die("usage: glt job JOB_ID")
		}
		stack = in(ui.NewTrace(ctx.Project, id))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", sub, usage)
		os.Exit(2)
	}

	ui.SetIcons(cfg.UI.Icons)
	ui.SetBackground(cfg.BackgroundEnabled())
	if !ui.SetTheme(cfg.UI.Theme) {
		fmt.Fprintf(os.Stderr, "glt: unknown theme %q, using the default (themes: %s)\n", cfg.UI.Theme, strings.Join(ui.ThemeNames(), ", "))
	}
	mouse := !noMouse && cfg.MouseEnabled()
	app := ui.New(ctx, client, stack, ui.Options{
		Notify:  notify || cfg.UI.Notify,
		Preview: cfg.PreviewEnabled(),
		Mouse:   mouse,
		Config:  cfg,
	})
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	if _, err := tea.NewProgram(app, opts...).Run(); err != nil {
		die("%v", err)
	}
}

func auth(cfg *config.File, pos []string, host string) {
	if len(pos) == 0 {
		die("usage: glt auth login [--host HOST] | glt auth status")
	}
	switch pos[0] {
	case "login":
		if host == "" {
			host = cfg.DefaultHost
		}
		web := "https://" + host
		if h, ok := cfg.Hosts[host]; ok {
			proto, api := h.APIProtocol, h.APIHost
			if proto == "" {
				proto = "https"
			}
			if api == "" {
				api = host
			}
			web = proto + "://" + api
		}
		fmt.Printf("Create a personal access token with the \"api\" scope:\n  %s/-/user_settings/personal_access_tokens?name=glt&scopes=api\n\n", web)
		fmt.Printf("Token for %s: ", host)
		var tok []byte
		var err error
		if term.IsTerminal(os.Stdin.Fd()) {
			tok, err = term.ReadPassword(os.Stdin.Fd())
			fmt.Println()
		} else {
			line, rerr := bufio.NewReader(os.Stdin).ReadString('\n')
			tok, err = []byte(line), rerr
		}
		token := strings.TrimSpace(string(tok))
		if token == "" {
			die("no token entered (%v)", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		h := cfg.Hosts[host]
		u, err := gitlab.New(web+"/api/v4", token, gitlab.TLSOptions{CACert: h.CACert, SkipVerify: h.SkipVerify}).CurrentUser(ctx)
		if err != nil {
			die("token check failed: %v", err)
		}
		if err := config.SetToken(host, token); err != nil {
			die("saving token: %v", err)
		}
		fmt.Printf("Logged in to %s as @%s (saved to %s)\n", host, u.Username, config.Path())
	case "status":
		names := make([]string, 0, len(cfg.Hosts))
		for n := range cfg.Hosts {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) == 0 {
			fmt.Println("No hosts configured. Run: glt auth login --host HOST")
			return
		}
		results := make([]string, len(names))
		var wg sync.WaitGroup
		for i, n := range names {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mark := " "
				if n == cfg.DefaultHost {
					mark = "*"
				}
				c, err := config.Resolve(cfg, n+"/x", "", "")
				if err != nil {
					results[i] = fmt.Sprintf("%s %s: %v", mark, n, err)
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				u, err := gitlab.New(c.APIBase, c.Token, tlsOpts(c)).CurrentUser(ctx)
				if err != nil {
					results[i] = fmt.Sprintf("%s %s: ✘ %v", mark, n, err)
					return
				}
				results[i] = fmt.Sprintf("%s %s: ✔ @%s", mark, n, u.Username)
			}()
		}
		wg.Wait()
		fmt.Println(strings.Join(results, "\n"))
		fmt.Println("\n* default host")
	default:
		die("unknown auth command %q", pos[0])
	}
}

func tlsOpts(c *config.Context) gitlab.TLSOptions {
	return gitlab.TLSOptions{CACert: c.TLS.CACert, SkipVerify: c.TLS.SkipVerify}
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "glt: "+f+"\n", a...)
	os.Exit(1)
}
