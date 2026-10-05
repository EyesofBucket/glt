package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// urlOpener returns the command that opens url in the user's browser.
func urlOpener(url string) *exec.Cmd {
	if b := os.Getenv("BROWSER"); b != "" {
		return exec.Command(b, url)
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url)
	case "windows":
		// rundll32 hands the URL to the default browser without a console
		// window (unlike "cmd /c start", which also mangles & in URLs)
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	return exec.Command("xdg-open", url)
}

// windowsToast shows a Windows 10+ toast using only built-in PowerShell
// and WinRT. The text is passed through the environment, not the script,
// so nothing in it can be interpreted as code. It borrows PowerShell's
// app ID, since unregistered app IDs don't get toasts.
const windowsToast = `
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
$t = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$x = $t.GetElementsByTagName('text')
$x.Item(0).AppendChild($t.CreateTextNode($env:GLT_TITLE)) > $null
$x.Item(1).AppendChild($t.CreateTextNode($env:GLT_BODY)) > $null
$app = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe'
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($app).Show([Windows.UI.Notifications.ToastNotification]::new($t))
`

// notifyDesktop shows a desktop notification where the platform has a way
// to; it's best-effort and silent on failure.
func notifyDesktop(title, body string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", windowsToast)
		cmd.Env = append(os.Environ(), "GLT_TITLE="+title, "GLT_BODY="+body)
	case "darwin":
		// likewise passed as arguments, not spliced into the script
		cmd = exec.Command("osascript", "-e", "on run argv", "-e",
			"display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", title, body)
	default:
		path, err := exec.LookPath("notify-send")
		if err != nil {
			return
		}
		cmd = exec.Command(path, "-a", "glt", title, body)
	}
	_ = cmd.Run()
}

// stateBase is where glt keeps per-user state (recent projects).
func stateBase() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return d
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state")
}
