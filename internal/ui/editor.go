package ui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Long text (MR descriptions, comments) can be written in the user's own
// editor: ctrl+e suspends glt, opens the text in a temporary file, and puts
// what was saved back into the field.

type editorMsg struct {
	text string
	err  error
	done func(string) tea.Cmd
}

// editorCommand finds the editor the way git does (GIT_EDITOR, core.editor,
// VISUAL, EDITOR), so it's the one used for commit messages.
func editorCommand() string {
	if out, err := exec.Command("git", "var", "GIT_EDITOR").Output(); err == nil {
		if e := strings.TrimSpace(string(out)); e != "" {
			return e
		}
	}
	for _, v := range []string{"VISUAL", "EDITOR"} {
		if e := strings.TrimSpace(os.Getenv(v)); e != "" {
			return e
		}
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// editorExec builds the command to edit path. The editor setting may
// carry arguments ("code --wait"), so on Unix it goes through the shell
// like git does. Windows has no such shell, so the setting is split here,
// keeping quoted paths whole: Git for Windows stores editors as e.g.
// "C:\Program Files\Notepad++\notepad++.exe" -multiInst -nosession.
func editorExec(editor, path string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		args := []string{editor}
		if _, err := os.Stat(editor); err != nil {
			args = splitCommand(editor)
		}
		return exec.Command(args[0], append(args[1:], path)...)
	}
	return exec.Command("sh", "-c", editor+` "$@"`, editor, path)
}

// splitCommand splits a command line on spaces, keeping "double" or
// 'single' quoted parts together. Backslashes are left alone, as they're
// Windows path separators.
func splitCommand(s string) []string {
	var out []string
	var cur strings.Builder
	quote, inArg := rune(0), false
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case r == ' ' || r == '\t':
			if inArg {
				out = append(out, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if inArg {
		out = append(out, cur.String())
	}
	if len(out) == 0 {
		out = []string{s}
	}
	return out
}

// editText opens text in the editor; done gets the saved text.
func (a *App) editText(text string, done func(string) tea.Cmd) tea.Cmd {
	f, err := os.CreateTemp("", "glt-*.md")
	if err != nil {
		a.setFlash("editor: "+err.Error(), true)
		return nil
	}
	path := f.Name()
	_, err = f.WriteString(text)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		a.setFlash("editor: "+err.Error(), true)
		return nil
	}
	return tea.ExecProcess(editorExec(editorCommand(), path), func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return editorMsg{err: err}
		}
		b, err := os.ReadFile(path)
		return editorMsg{text: strings.TrimRight(string(b), "\r\n"), err: err, done: done}
	})
}

func (a *App) editorDone(msg editorMsg) tea.Cmd {
	var cmds []tea.Cmd
	// handing the terminal back turns mouse reporting off
	if a.opts.Mouse {
		cmds = append(cmds, tea.EnableMouseCellMotion)
	}
	if msg.err != nil {
		a.setFlash("editor: "+msg.err.Error(), true)
	} else if msg.done != nil {
		cmds = append(cmds, msg.done(msg.text))
	}
	return tea.Batch(cmds...)
}
