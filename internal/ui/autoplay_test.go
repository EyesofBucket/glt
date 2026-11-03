package ui

import (
	"testing"

	"gitlab-tui/internal/gitlab"
)

func TestAutoPlay(t *testing.T) {
	a := &App{autoPlay: map[int]autoPlayJob{}, store: &Store{m: map[string]*entry{}}}
	deploy := &gitlab.Job{ID: 1, Name: "deploy", Status: "created"}

	// p on a created job flags it, and p again unflags it
	if a.playJob("g/p", 9, deploy, nil) != nil {
		t.Fatal("flagging a created job shouldn't call the API")
	}
	if got := a.shownStatus(deploy); got != "auto-play" {
		t.Fatalf("flagged status = %q", got)
	}
	a.playJob("g/p", 9, deploy, nil)
	if got := a.shownStatus(deploy); got != "created" {
		t.Fatalf("unflagged status = %q", got)
	}
	if a.playJob("g/p", 9, &gitlab.Job{ID: 2, Name: "x", Status: "success"}, nil) != nil || len(a.autoPlay) != 0 {
		t.Fatal("only created jobs can be flagged")
	}

	// while still created, nothing happens; once manual, it's played and unflagged
	a.playJob("g/p", 9, deploy, nil)
	if a.pollAutoPlay() == nil {
		t.Fatal("expected a jobs fetch for the flagged pipeline")
	}
	if a.checkAutoPlay(resultMsg{val: []gitlab.Job{*deploy}}) != nil || len(a.autoPlay) != 1 {
		t.Fatal("a created job shouldn't be played yet")
	}
	if a.checkAutoPlay(resultMsg{val: []gitlab.Job{*deploy}, disk: true}) != nil {
		t.Fatal("cached results shouldn't trigger anything")
	}
	manual := *deploy
	manual.Status = "manual"
	if a.checkAutoPlay(resultMsg{val: &manual}) == nil {
		t.Fatal("expected a play once the job is manual")
	}
	if len(a.autoPlay) != 0 {
		t.Fatal("flag should be cleared after playing")
	}

	// a job that skips past manual just loses its flag
	a.playJob("g/p", 9, deploy, nil)
	skipped := *deploy
	skipped.Status = "skipped"
	if a.checkAutoPlay(resultMsg{val: []gitlab.Job{skipped}}) != nil || len(a.autoPlay) != 0 {
		t.Fatal("a skipped job should be unflagged without playing")
	}
}
