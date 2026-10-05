// Package config loads glt's own config file (~/.config/glt/config.yml) and
// works out which GitLab host/project/branch to talk to.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Host struct {
	Token       string `yaml:"token"`
	APIHost     string `yaml:"api_host,omitempty"`
	APIProtocol string `yaml:"api_protocol,omitempty"`
	SSHHost     string `yaml:"ssh_host,omitempty"`
	CACert      string `yaml:"ca_cert,omitempty"`
	SkipVerify  bool   `yaml:"skip_tls_verify,omitempty"`
}

type UI struct {
	Icons string `yaml:"icons"` // nerd | unicode
	Theme string `yaml:"theme"` // colour theme; "default" uses the terminal's colours
	// Background: whether true-colour themes paint their own background
	Background *bool  `yaml:"background"`
	Picker     string `yaml:"picker"`  // project picker view: list | tree
	Mouse      *bool  `yaml:"mouse"`   // clicks, wheel scrolling and drag-to-copy
	Preview    *bool  `yaml:"preview"` // log preview pane in the pipeline view
	Notify     bool   `yaml:"notify"`  // desktop notification when a watched pipeline finishes
}

type File struct {
	DefaultHost string          `yaml:"default_host"`
	Hosts       map[string]Host `yaml:"hosts"`
	UI          UI              `yaml:"ui"`
}

func (f *File) MouseEnabled() bool   { return f.UI.Mouse == nil || *f.UI.Mouse }
func (f *File) PreviewEnabled() bool { return f.UI.Preview == nil || *f.UI.Preview }
func (f *File) BackgroundEnabled() bool {
	return f.UI.Background == nil || *f.UI.Background
}

// Context is the resolved session target.
type Context struct {
	Host    string // config key, e.g. gitlab.com
	APIBase string // e.g. https://gitlab.com/api/v4
	WebBase string // e.g. https://gitlab.com
	Token   string
	Project string // full path of the cwd/-R project; empty when there isn't one
	Branch  string // current branch, may be empty
	Remote  string // git remote name used, may be empty
	TLS     TLS
}

type TLS struct {
	CACert     string // PEM file to trust in addition to the system roots
	SkipVerify bool
}

func Dir() string {
	if d := os.Getenv("GLT_CONFIG_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "glt")
	}
	if runtime.GOOS == "windows" {
		if d, err := os.UserConfigDir(); err == nil { // %AppData%
			return filepath.Join(d, "glt")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "glt")
}

func Path() string { return filepath.Join(Dir(), "config.yml") }

// Load reads the config, creating the directory and a commented default file
// on first run. If glab is configured, its hosts and tokens are imported
// once so glt works straight away; after that glt never reads glab's config.
// notice is a one-line message for the user when a file was created.
func Load() (cfg *File, notice string, err error) {
	p := Path()
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		cfg = &File{DefaultHost: "gitlab.com", Hosts: map[string]Host{}, UI: UI{Icons: "nerd", Theme: "default", Picker: "list"}}
		imported := importGlab(cfg)
		if err := os.MkdirAll(Dir(), 0o700); err != nil {
			return nil, "", err
		}
		if err := os.WriteFile(p, []byte(render(cfg)), 0o600); err != nil {
			return nil, "", err
		}
		notice = "created " + p
		if imported > 0 {
			notice += fmt.Sprintf(" (imported %d host(s) from glab)", imported)
		}
		return cfg, notice, nil
	}
	if err != nil {
		return nil, "", err
	}
	cfg = &File{}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, "", fmt.Errorf("%s: %w", p, err)
	}
	if cfg.DefaultHost == "" {
		cfg.DefaultHost = "gitlab.com"
	}
	if cfg.Hosts == nil {
		cfg.Hosts = map[string]Host{}
	}
	if cfg.UI.Icons == "" {
		cfg.UI.Icons = "nerd"
	}
	if cfg.UI.Theme == "" {
		cfg.UI.Theme = "default"
	}
	if cfg.UI.Picker == "" {
		cfg.UI.Picker = "list"
	}
	return cfg, "", nil
}

// importGlab copies hosts from glab's config into cfg, returning how many.
func importGlab(cfg *File) int {
	var dirs []string
	switch {
	case os.Getenv("GLAB_CONFIG_DIR") != "":
		dirs = []string{os.Getenv("GLAB_CONFIG_DIR")}
	case os.Getenv("XDG_CONFIG_HOME") != "":
		dirs = []string{filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "glab-cli")}
	default:
		home, _ := os.UserHomeDir()
		dirs = []string{filepath.Join(home, ".config", "glab-cli")}
		if d, err := os.UserConfigDir(); err == nil && runtime.GOOS == "windows" {
			dirs = append(dirs, filepath.Join(d, "glab-cli"))
		}
	}
	var b []byte
	err := os.ErrNotExist
	for _, dir := range dirs {
		if b, err = os.ReadFile(filepath.Join(dir, "config.yml")); err == nil {
			break
		}
	}
	if err != nil {
		return 0
	}
	var g struct {
		Hosts map[string]Host `yaml:"hosts"`
	}
	if yaml.Unmarshal(b, &g) != nil {
		return 0
	}
	n := 0
	for name, h := range g.Hosts {
		if h.Token == "" {
			continue
		}
		if h.APIHost == name {
			h.APIHost = ""
		}
		if h.APIProtocol == "https" {
			h.APIProtocol = ""
		}
		cfg.Hosts[name] = h
		n++
	}
	return n
}

const header = `# glt configuration
#
# default_host is used when the current directory isn't a git repository
# whose remote points at one of the hosts below.
#
# Tokens need the "api" scope. Set one with:  glt auth login --host HOST
# The GITLAB_TOKEN environment variable overrides the token for any host.
#
# Per-host options (all optional except token):
#   api_host:     API hostname if different from the host name
#   api_protocol: http or https (default https)
#   ssh_host:     hostname used in SSH git remotes, if it's an alias
#   ca_cert:      PEM file for a self-signed instance
#   skip_tls_verify: true to disable certificate checks (not recommended)
#
# ui.icons: "nerd" needs a Nerd Font; "unicode" works everywhere.
# ui.theme: default (the terminal's own colours), catppuccin-mocha,
#   catppuccin-macchiato, catppuccin-frappe, catppuccin-latte, github-dark,
#   github-light, gruvbox-dark, gruvbox-light, hackerman, miasma, kanagawa,
#   kanagawa-dragon, kanagawa-lotus, noctis, rose-pine, rose-pine-moon,
#   rose-pine-dawn, shades-of-purple, tokyonight, tokyonight-storm,
#   tokyonight-moon, tokyonight-day. Press t in glt to try them, and ","
#   for settings.
# ui.background: false keeps the terminal's own background (and any
#   transparency) under a theme instead of painting the theme's.
# ui.picker: how the project picker (P) opens: "list" (fuzzy list) or
#   "tree" (projects grouped by GitLab group). ctrl+t switches in the picker.

`

func render(cfg *File) string {
	type out struct {
		DefaultHost string          `yaml:"default_host"`
		Hosts       map[string]Host `yaml:"hosts"`
		UI          struct {
			Icons      string `yaml:"icons"`
			Theme      string `yaml:"theme"`
			Background bool   `yaml:"background"`
			Picker     string `yaml:"picker"`
			Mouse      bool   `yaml:"mouse"`
			Preview    bool   `yaml:"preview"`
			Notify     bool   `yaml:"notify"`
		} `yaml:"ui"`
	}
	o := out{DefaultHost: cfg.DefaultHost, Hosts: cfg.Hosts}
	o.UI.Theme, o.UI.Background, o.UI.Picker = cfg.UI.Theme, cfg.BackgroundEnabled(), cfg.UI.Picker
	o.UI.Icons, o.UI.Mouse, o.UI.Preview, o.UI.Notify = cfg.UI.Icons, cfg.MouseEnabled(), cfg.PreviewEnabled(), cfg.UI.Notify
	b, _ := yaml.Marshal(o)
	return header + string(b)
}

// SetToken stores a host token, editing the YAML in place so the user's
// comments and other settings survive.
func SetToken(host, token string) error {
	p := Path()
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	hosts := mapChild(doc.Content[0], "hosts")
	h := mapChild(hosts, host)
	tok := mapChild(h, "token")
	tok.Kind, tok.Tag, tok.Value, tok.Style = yaml.ScalarNode, "!!str", token, 0
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	return os.WriteFile(p, out, 0o600)
}

// Set stores value at a key path (e.g. "ui", "theme"), editing the YAML in
// place so comments and other settings survive.
func Set(value any, path ...string) error {
	p := Path()
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 {
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	n := doc.Content[0]
	for _, k := range path {
		n = mapChild(n, k)
	}
	var v yaml.Node
	if err := v.Encode(value); err != nil {
		return err
	}
	n.Kind, n.Tag, n.Value, n.Style, n.Content = v.Kind, v.Tag, v.Value, v.Style, v.Content
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	return os.WriteFile(p, out, 0o600)
}

// mapChild returns the value node for key in mapping m, creating it.
func mapChild(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		m.Kind, m.Tag, m.Value, m.Content = yaml.MappingNode, "!!map", "", nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	v := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	m.Content = append(m.Content, k, v)
	return v
}

func git(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ParseRemote splits a git remote URL into host and project path.
func ParseRemote(raw string) (host, path string, err error) {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.Contains(raw, "://"):
		u, perr := url.Parse(raw)
		if perr != nil {
			return "", "", perr
		}
		host, path = u.Hostname(), u.Path
	case strings.Contains(raw, ":"): // scp-like: git@host:group/project.git
		i := strings.Index(raw, ":")
		host, path = raw[:i], raw[i+1:]
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
	default:
		return "", "", fmt.Errorf("unrecognised remote URL %q", raw)
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if host == "" || path == "" {
		return "", "", fmt.Errorf("unrecognised remote URL %q", raw)
	}
	return host, path, nil
}

// Resolve picks the host and (optional) project. repo may be
// "group/project" or "host/group/project"; empty means use the git remote
// of the cwd if it belongs to a known GitLab host, else no project on the
// default host.
func Resolve(cfg *File, repo, branch, host string) (*Context, error) {
	ctx := &Context{Branch: branch}
	if repo != "" {
		parts := strings.SplitN(repo, "/", 2)
		// a leading component that looks like a hostname selects the host
		if host == "" && len(parts) == 2 && strings.Contains(parts[0], ".") {
			host, repo = parts[0], parts[1]
		}
		ctx.Project = strings.Trim(repo, "/")
	} else if h, p, remote, br := detectRepo(cfg, branch); p != "" && (host == "" || host == h) {
		host, ctx.Project, ctx.Remote, ctx.Branch = h, p, remote, br
	}
	if host == "" {
		host = cfg.DefaultHost
	}
	ctx.Host = host

	h := cfg.Hosts[host]
	proto := h.APIProtocol
	if proto == "" {
		proto = "https"
	}
	apiHost := h.APIHost
	if apiHost == "" {
		apiHost = host
	}
	ctx.TLS = TLS{CACert: h.CACert, SkipVerify: h.SkipVerify}
	ctx.WebBase = proto + "://" + apiHost
	ctx.APIBase = ctx.WebBase + "/api/v4"

	for _, env := range []string{"GITLAB_TOKEN", "GLT_TOKEN"} {
		if t := os.Getenv(env); t != "" {
			ctx.Token = t
			break
		}
	}
	if ctx.Token == "" {
		ctx.Token = h.Token
	}
	if ctx.Token == "" {
		return nil, fmt.Errorf("no token for %s: run `glt auth login --host %s` or set GITLAB_TOKEN", host, host)
	}
	return ctx, nil
}

// detectRepo returns the GitLab host and project of the cwd's git remote,
// if the remote points at a host glt knows about.
func detectRepo(cfg *File, branch string) (host, project, remote, br string) {
	br = branch
	if br == "" {
		br = git("branch", "--show-current")
	}
	if br != "" {
		remote = git("config", "branch."+br+".remote")
	}
	if remote == "" {
		remote = "origin"
	}
	u := git("remote", "get-url", remote)
	if u == "" {
		if rs := strings.Fields(git("remote")); len(rs) > 0 {
			remote = rs[0]
			u = git("remote", "get-url", remote)
		}
	}
	if u == "" {
		return "", "", "", br
	}
	rh, rp, err := ParseRemote(u)
	if err != nil {
		return "", "", "", br
	}
	if h := matchHost(cfg, rh); h != "" {
		return h, rp, remote, br
	}
	return "", "", "", br
}

// matchHost maps a git remote host (which may be an SSH alias) to a
// configured host, or "" if it isn't a GitLab host we know.
func matchHost(cfg *File, remoteHost string) string {
	if _, ok := cfg.Hosts[remoteHost]; ok {
		return remoteHost
	}
	names := make([]string, 0, len(cfg.Hosts))
	for name := range cfg.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		h := cfg.Hosts[name]
		if h.SSHHost == remoteHost || h.APIHost == remoteHost {
			return name
		}
	}
	if remoteHost == cfg.DefaultHost || remoteHost == "gitlab.com" {
		return remoteHost
	}
	return ""
}
