package ui

import (
	"reflect"
	"testing"
)

func TestLabelToggleScopedAndDiff(t *testing.T) {
	ch := &chooser{multi: true, exclusive: scopedSiblings,
		orig: map[string]bool{"bug": true, "prio::high": true}, on: map[string]bool{"bug": true, "prio::high": true}}
	ch.toggle("prio::low") // replaces prio::high
	ch.toggle("bug")       // off
	ch.toggle("backend")   // on
	ch.toggle("team::a::x")
	if ch.on["prio::high"] || !ch.on["prio::low"] {
		t.Fatalf("scoped label not exclusive: %v", ch.on)
	}
	add, remove := ch.diff()
	if want := []string{"backend", "prio::low", "team::a::x"}; !reflect.DeepEqual(add, want) {
		t.Errorf("add = %v, want %v", add, want)
	}
	if want := []string{"bug", "prio::high"}; !reflect.DeepEqual(remove, want) {
		t.Errorf("remove = %v, want %v", remove, want)
	}
}
