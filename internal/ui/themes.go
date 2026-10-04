package ui

import "strings"

// Theme is glt's colour palette. The default theme uses the terminal's own
// 16 ANSI colours (so it follows whatever the terminal is set to and works
// everywhere); the others are true-colour palettes taken from each theme's
// published colours, and paint their own background.
type Theme struct {
	Name, Label string
	Light       bool
	Bg, Fg      string // "" means the terminal's default
	Surface     string // selection band, status line band
	Muted       string // secondary text, borders
	Red         string
	Green       string
	Yellow      string
	Blue        string
	Magenta     string
	Cyan        string
	Orange      string // allowed-to-fail jobs
	// Running is the colour of running pipelines and jobs; empty means Blue.
	// Themes whose "blue" isn't blue (Hackerman's is green) set it so
	// running stays distinct from success.
	Running string
}

// hex reports whether the theme uses true colour (and paints a background).
func (t Theme) hex() bool { return strings.HasPrefix(t.Red, "#") }

// ansi16 maps the terminal palette onto the theme, so ANSI colours in job
// logs match it too.
func (t Theme) ansi16() [16]string {
	fg := t.Fg
	return [16]string{
		t.Surface, t.Red, t.Green, t.Yellow, t.Blue, t.Magenta, t.Cyan, fg,
		t.Muted, t.Red, t.Green, t.Orange, t.Blue, t.Magenta, t.Cyan, fg,
	}
}

var themes = []Theme{
	{Name: "default", Label: "Default (terminal colours)", Surface: "0", Muted: "8",
		Red: "1", Green: "2", Yellow: "3", Blue: "4", Magenta: "5", Cyan: "6", Orange: "11"},

	// https://github.com/catppuccin/palette
	{Name: "catppuccin-mocha", Label: "Catppuccin Mocha", Bg: "#1e1e2e", Fg: "#cdd6f4", Surface: "#45475a", Muted: "#7f849c",
		Red: "#f38ba8", Green: "#a6e3a1", Yellow: "#f9e2af", Blue: "#89b4fa", Magenta: "#cba6f7", Cyan: "#94e2d5", Orange: "#fab387"},
	{Name: "catppuccin-macchiato", Label: "Catppuccin Macchiato", Bg: "#24273a", Fg: "#cad3f5", Surface: "#494d64", Muted: "#8087a2",
		Red: "#ed8796", Green: "#a6da95", Yellow: "#eed49f", Blue: "#8aadf4", Magenta: "#c6a0f6", Cyan: "#8bd5ca", Orange: "#f5a97f"},
	{Name: "catppuccin-frappe", Label: "Catppuccin Frappé", Bg: "#303446", Fg: "#c6d0f5", Surface: "#51576d", Muted: "#838ba7",
		Red: "#e78284", Green: "#a6d189", Yellow: "#e5c890", Blue: "#8caaee", Magenta: "#ca9ee6", Cyan: "#81c8be", Orange: "#ef9f76"},
	{Name: "catppuccin-latte", Label: "Catppuccin Latte", Light: true, Bg: "#eff1f5", Fg: "#4c4f69", Surface: "#ccd0da", Muted: "#8c8fa1",
		Red: "#d20f39", Green: "#40a02b", Yellow: "#df8e1d", Blue: "#1e66f5", Magenta: "#8839ef", Cyan: "#179299", Orange: "#fe640b"},

	// https://github.com/projekt0n/github-nvim-theme (primitives)
	{Name: "github-dark", Label: "GitHub Dark", Bg: "#0d1117", Fg: "#e6edf3", Surface: "#21262d", Muted: "#848d97",
		Red: "#f85149", Green: "#3fb950", Yellow: "#d29922", Blue: "#58a6ff", Magenta: "#bc8cff", Cyan: "#56d4dd", Orange: "#f0883e"},
	{Name: "github-light", Label: "GitHub Light", Light: true, Bg: "#ffffff", Fg: "#1f2328", Surface: "#eaeef2", Muted: "#656d76",
		Red: "#cf222e", Green: "#1a7f37", Yellow: "#9a6700", Blue: "#0969da", Magenta: "#8250df", Cyan: "#1b7c83", Orange: "#bc4c00"},

	// https://github.com/morhetz/gruvbox
	{Name: "gruvbox-dark", Label: "Gruvbox Dark", Bg: "#282828", Fg: "#ebdbb2", Surface: "#3c3836", Muted: "#928374",
		Red: "#fb4934", Green: "#b8bb26", Yellow: "#fabd2f", Blue: "#83a598", Magenta: "#d3869b", Cyan: "#8ec07c", Orange: "#fe8019"},
	{Name: "gruvbox-light", Label: "Gruvbox Light", Light: true, Bg: "#fbf1c7", Fg: "#3c3836", Surface: "#ebdbb2", Muted: "#928374",
		Red: "#9d0006", Green: "#79740e", Yellow: "#b57614", Blue: "#076678", Magenta: "#8f3f71", Cyan: "#427b58", Orange: "#af3a03"},

	// https://github.com/xero/miasma.nvim (extras/miasma.ghostty); its
	// palette is earthy, so its oranges stand in for red
	{Name: "miasma", Label: "Miasma", Bg: "#222222", Fg: "#c2c2b0", Surface: "#3a3a3a", Muted: "#666666",
		Red: "#b36d43", Green: "#5f875f", Yellow: "#c9a554", Blue: "#78824b", Magenta: "#bb7744", Cyan: "#d7c483", Orange: "#685742"},

	// Omarchy's Hackerman (basecamp/omarchy themes/hackerman/colors.toml,
	// also bjarneo/hackerman.nvim): near-monochrome green. Its accent green
	// takes the place of blue (keys, active borders, tabs, the mode block),
	// running is its cyan, and since its own "red" is green, failures and
	// conflicts get a real red so they still stand out.
	{Name: "hackerman", Label: "Hackerman", Bg: "#0b0c16", Fg: "#ddf7ff", Surface: "#1f253a", Muted: "#6a6e95",
		Red: "#ff4f6e", Green: "#4fe88f", Yellow: "#50f7d4", Blue: "#82fb9c", Magenta: "#86a7df", Cyan: "#7cf8f7", Orange: "#50f7a3",
		Running: "#7cf8f7"},

	// https://github.com/rebelot/kanagawa.nvim
	{Name: "kanagawa", Label: "Kanagawa Wave", Bg: "#1f1f28", Fg: "#dcd7ba", Surface: "#2d4f67", Muted: "#727169",
		Red: "#e46876", Green: "#98bb6c", Yellow: "#e6c384", Blue: "#7e9cd8", Magenta: "#957fb8", Cyan: "#7aa89f", Orange: "#ffa066"},
	{Name: "kanagawa-dragon", Label: "Kanagawa Dragon", Bg: "#181616", Fg: "#c5c9c5", Surface: "#282727", Muted: "#a6a69c",
		Red: "#c4746e", Green: "#8a9a7b", Yellow: "#c4b28a", Blue: "#8ba4b0", Magenta: "#8992a7", Cyan: "#8ea4a2", Orange: "#b6927b"},
	{Name: "kanagawa-lotus", Label: "Kanagawa Lotus", Light: true, Bg: "#f2ecbc", Fg: "#545464", Surface: "#e7dba0", Muted: "#8a8980",
		Red: "#c84053", Green: "#6f894e", Yellow: "#77713f", Blue: "#4d699b", Magenta: "#624c83", Cyan: "#597b75", Orange: "#cc6d00"},

	// https://github.com/liviuschera/noctis
	{Name: "noctis", Label: "Noctis", Bg: "#052529", Fg: "#b2cacd", Surface: "#0b515b", Muted: "#5b858b",
		Red: "#e66533", Green: "#49e9a6", Yellow: "#e4b781", Blue: "#49ace9", Magenta: "#df769b", Cyan: "#49d6e9", Orange: "#e69533"},

	// https://rosepinetheme.com/palette
	{Name: "rose-pine", Label: "Rosé Pine", Bg: "#191724", Fg: "#e0def4", Surface: "#403d52", Muted: "#6e6a86",
		Red: "#eb6f92", Green: "#31748f", Yellow: "#f6c177", Blue: "#9ccfd8", Magenta: "#c4a7e7", Cyan: "#ebbcba", Orange: "#f6c177"},
	{Name: "rose-pine-moon", Label: "Rosé Pine Moon", Bg: "#232136", Fg: "#e0def4", Surface: "#44415a", Muted: "#6e6a86",
		Red: "#eb6f92", Green: "#3e8fb0", Yellow: "#f6c177", Blue: "#9ccfd8", Magenta: "#c4a7e7", Cyan: "#ea9a97", Orange: "#f6c177"},
	{Name: "rose-pine-dawn", Label: "Rosé Pine Dawn", Light: true, Bg: "#faf4ed", Fg: "#575279", Surface: "#dfdad9", Muted: "#9893a5",
		Red: "#b4637a", Green: "#286983", Yellow: "#ea9d34", Blue: "#56949f", Magenta: "#907aa9", Cyan: "#d7827e", Orange: "#ea9d34"},

	// https://github.com/ahmadawais/shades-of-purple-vscode
	{Name: "shades-of-purple", Label: "Shades of Purple", Bg: "#2d2b55", Fg: "#ffffff", Surface: "#1e1e3f", Muted: "#a599e9",
		Red: "#ec3a37", Green: "#3ad900", Yellow: "#fad000", Blue: "#9effff", Magenta: "#ff2c70", Cyan: "#80fcff", Orange: "#ff9d00"},

	// https://github.com/folke/tokyonight.nvim
	{Name: "tokyonight", Label: "Tokyo Night", Bg: "#1a1b26", Fg: "#c0caf5", Surface: "#292e42", Muted: "#565f89",
		Red: "#f7768e", Green: "#9ece6a", Yellow: "#e0af68", Blue: "#7aa2f7", Magenta: "#bb9af7", Cyan: "#7dcfff", Orange: "#ff9e64"},
	{Name: "tokyonight-storm", Label: "Tokyo Night Storm", Bg: "#24283b", Fg: "#c0caf5", Surface: "#292e42", Muted: "#565f89",
		Red: "#f7768e", Green: "#9ece6a", Yellow: "#e0af68", Blue: "#7aa2f7", Magenta: "#bb9af7", Cyan: "#7dcfff", Orange: "#ff9e64"},
	{Name: "tokyonight-moon", Label: "Tokyo Night Moon", Bg: "#222436", Fg: "#c8d3f5", Surface: "#2f334d", Muted: "#636da6",
		Red: "#ff757f", Green: "#c3e88d", Yellow: "#ffc777", Blue: "#82aaff", Magenta: "#c099ff", Cyan: "#86e1fc", Orange: "#ff966c"},
	{Name: "tokyonight-day", Label: "Tokyo Night Day", Light: true, Bg: "#e1e2e7", Fg: "#3760bf", Surface: "#c4c8da", Muted: "#848cb5",
		Red: "#f52a65", Green: "#587539", Yellow: "#8c6c3e", Blue: "#2e7de9", Magenta: "#9854f1", Cyan: "#007197", Orange: "#b15c00"},
}

func findTheme(name string) (Theme, bool) {
	for _, t := range themes {
		if t.Name == name {
			return t, true
		}
	}
	return themes[0], false
}

// ThemeNames lists the available themes.
func ThemeNames() []string {
	out := make([]string, len(themes))
	for i, t := range themes {
		out[i] = t.Name
	}
	return out
}
