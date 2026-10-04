package config

import (
	"os"
	"strings"
	"testing"
)

func TestParseRemote(t *testing.T) {
	cases := []struct{ in, host, path string }{
		{"git@gitlab.com:group/sub/proj.git", "gitlab.com", "group/sub/proj"},
		{"https://gitlab.com/group/proj", "gitlab.com", "group/proj"},
		{"https://gitlab.com/group/proj.git/", "gitlab.com", "group/proj"},
		{"ssh://git@gitlab.example.com:2222/group/proj.git", "gitlab.example.com", "group/proj"},
		{"https://oauth2:tok@gitlab.com/a/b.git", "gitlab.com", "a/b"},
	}
	for _, c := range cases {
		h, p, err := ParseRemote(c.in)
		if err != nil || h != c.host || p != c.path {
			t.Errorf("ParseRemote(%q) = %q, %q, %v", c.in, h, p, err)
		}
	}
}

func TestLoadCreatesAndImports(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GLT_CONFIG_DIR", dir+"/glt")
	t.Setenv("GLAB_CONFIG_DIR", dir+"/glab")
	if err := os.MkdirAll(dir+"/glab", 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(dir+"/glab/config.yml", []byte("host: gitlab.com\nhosts:\n  gitlab.com:\n    token: abc\n    api_host: gitlab.com\n  example.org:\n    token: def\n    ssh_host: git.example.org\n"), 0o600)

	cfg, notice, err := Load()
	if err != nil || notice == "" {
		t.Fatalf("Load: %v %q", err, notice)
	}
	if cfg.DefaultHost != "gitlab.com" || cfg.Hosts["gitlab.com"].Token != "abc" || cfg.Hosts["example.org"].SSHHost != "git.example.org" {
		t.Fatalf("unexpected config %+v", cfg)
	}
	info, err := os.Stat(Path())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config file mode: %v %v", info, err)
	}
	// second load reads the file, no notice
	cfg2, notice, err := Load()
	if err != nil || notice != "" || cfg2.Hosts["example.org"].Token != "def" {
		t.Fatalf("reload: %v %q %+v", err, notice, cfg2)
	}
	if h := matchHost(cfg2, "git.example.org"); h != "example.org" {
		t.Errorf("ssh alias match: %q", h)
	}
	if h := matchHost(cfg2, "github.com"); h != "" {
		t.Errorf("github.com should not match a GitLab host: %q", h)
	}

	if err := SetToken("new.host", "xyz"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path())
	if !strings.Contains(string(b), "# glt configuration") {
		t.Error("SetToken dropped the header comments")
	}
	cfg3, _, _ := Load()
	if cfg3.Hosts["new.host"].Token != "xyz" || cfg3.Hosts["gitlab.com"].Token != "abc" {
		t.Errorf("after SetToken: %+v", cfg3.Hosts)
	}
}

func TestSetKeepsComments(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GLT_CONFIG_DIR", dir)
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir())
	if _, _, err := Load(); err != nil {
		t.Fatal(err)
	}
	if err := Set("tokyonight", "ui", "theme"); err != nil {
		t.Fatal(err)
	}
	if err := Set(false, "ui", "mouse"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Theme != "tokyonight" || cfg.MouseEnabled() {
		t.Errorf("got theme %q mouse %v", cfg.UI.Theme, cfg.MouseEnabled())
	}
	b, _ := os.ReadFile(Path())
	if !strings.Contains(string(b), "# glt configuration") {
		t.Error("header comment lost")
	}
}
