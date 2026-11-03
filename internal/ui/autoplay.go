package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gitlab-tui/internal/gitlab"
)

// autoPlayJob is a created job to play as soon as GitLab makes it manual.
// Flags last for the session: nothing can play the job once glt exits.
type autoPlayJob struct {
	project  string
	pipeline int
	name     string
}

// shownStatus is the job's status as glt displays it: a flagged created
// job shows as "auto-play".
func (a *App) shownStatus(j *gitlab.Job) string {
	if _, ok := a.autoPlay[j.ID]; ok && j.Status == "created" {
		return "auto-play"
	}
	return j.Status
}

// playJob plays a manual job, or toggles auto-play on a created one in
// pipeline pl.
func (a *App) playJob(p string, pl int, j *gitlab.Job, inval []string) tea.Cmd {
	id, name := j.ID, j.Name
	switch j.Status {
	case "manual":
		delete(a.autoPlay, id)
		return a.action("play "+name, func() error { _, err := a.client.PlayJob(bg(), p, id); return err }, inval, nil)
	case "created":
		if _, ok := a.autoPlay[id]; ok {
			delete(a.autoPlay, id)
			a.setFlash("auto-play off for "+name, false)
			return nil
		}
		a.autoPlay[id] = autoPlayJob{project: p, pipeline: pl, name: name}
		a.setFlash(name+" will play when it becomes manual", false)
		return nil
	}
	a.setFlash(name+" is not a manual or created job", true)
	return nil
}

// pollAutoPlay keeps the jobs of pipelines with flagged jobs fresh, even
// when no view is showing them.
func (a *App) pollAutoPlay() tea.Cmd {
	var cmds []tea.Cmd
	seen := map[string]bool{}
	for _, f := range a.autoPlay {
		k := jobsKey(f.project, f.pipeline)
		if f.pipeline == 0 || seen[k] {
			continue
		}
		seen[k] = true
		p, pl := f.project, f.pipeline
		cmds = append(cmds, fetch(a.store, k, 5*time.Second, func(ctx ctxT) ([]gitlab.Job, error) {
			return a.client.ListPipelineJobs(ctx, p, pl)
		}))
	}
	return tea.Batch(cmds...)
}

// checkAutoPlay plays flagged jobs that have become manual, and drops
// flags on jobs that can no longer become manual.
func (a *App) checkAutoPlay(msg resultMsg) tea.Cmd {
	if len(a.autoPlay) == 0 || msg.disk || msg.err != nil {
		return nil
	}
	var jobs []gitlab.Job
	switch v := msg.val.(type) {
	case []gitlab.Job:
		jobs = v
	case *gitlab.Job:
		if v != nil {
			jobs = []gitlab.Job{*v}
		}
	}
	var cmds []tea.Cmd
	for _, j := range jobs {
		f, ok := a.autoPlay[j.ID]
		if !ok || j.Status == "created" {
			continue
		}
		delete(a.autoPlay, j.ID)
		if j.Status != "manual" {
			a.setFlash("auto-play off: "+f.name+" is "+j.Status, false)
			continue
		}
		p, id := f.project, j.ID
		inval := a.keysWithPrefix("jobs:", "pipe:", jobKey(p, id))
		cmds = append(cmds, a.action("auto-play "+f.name, func() error { _, err := a.client.PlayJob(bg(), p, id); return err }, inval, nil))
	}
	return tea.Batch(cmds...)
}
