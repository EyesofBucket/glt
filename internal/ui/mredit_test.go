package ui

import (
	"reflect"
	"testing"

	"gitlab-tui/internal/gitlab"
)

func editTestMR() *gitlab.MR {
	return &gitlab.MR{
		IID: 7, Title: "[Draft] Fix the thing", Draft: true, Description: "body",
		SourceBranch: "fix", TargetBranch: "main", Labels: []string{"a", "b"},
		Assignees: []gitlab.User{{ID: 1, Username: "me"}},
		Reviewers: []gitlab.User{{ID: 2, Username: "rev"}},
		Squash:    true,
	}
}

func TestEditUnchanged(t *testing.T) {
	v := newMREditForm("g/p", editTestMR())
	v.labels = []string{"b", "a"} // order doesn't matter
	if c := v.changes(); len(c) != 0 {
		t.Fatalf("changes = %v, want none (and the [Draft] spelling kept)", c)
	}
	if v.dirty() {
		t.Fatal("dirty with no changes")
	}
}

func TestEditChanges(t *testing.T) {
	v := newMREditForm("g/p", editTestMR())
	v.draft = false
	v.assignees = nil
	v.reviewers = append(v.reviewers, gitlab.User{ID: 3})
	v.desc.SetValue("new body")
	v.squash = false
	want := map[string]any{
		"title":        "Fix the thing",
		"assignee_ids": []int{0},
		"reviewer_ids": []int{2, 3},
		"description":  "new body",
		"squash":       false,
	}
	if c := v.changes(); !reflect.DeepEqual(c, want) {
		t.Fatalf("changes = %#v\nwant %#v", c, want)
	}
	v.draft = true
	if got := v.changes()["title"]; got != nil {
		t.Fatalf("title sent (%v) though only the draft spelling would change", got)
	}
}
