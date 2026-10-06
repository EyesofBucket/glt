package ui

import (
	"reflect"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"vim", []string{"vim"}},
		{"code --wait", []string{"code", "--wait"}},
		{`"C:\Program Files\Notepad++\notepad++.exe" -multiInst -notabbar -nosession -noPlugin`,
			[]string{`C:\Program Files\Notepad++\notepad++.exe`, "-multiInst", "-notabbar", "-nosession", "-noPlugin"}},
		{`'C:\Users\me\AppData\Local\Programs\Microsoft VS Code\bin\code'  --wait`,
			[]string{`C:\Users\me\AppData\Local\Programs\Microsoft VS Code\bin\code`, "--wait"}},
		{`notepad ""`, []string{"notepad", ""}},
	} {
		if got := splitCommand(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitCommand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
