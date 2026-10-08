package gitlab

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

type Pipeline struct {
	ID         int        `json:"id"`
	IID        int        `json:"iid"`
	ProjectID  int        `json:"project_id"`
	Status     string     `json:"status"`
	Ref        string     `json:"ref"`
	SHA        string     `json:"sha"`
	Source     string     `json:"source"`
	WebURL     string     `json:"web_url"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Duration   float64    `json:"duration"`
	User       *User      `json:"user"`
}

type Runner struct {
	Description string `json:"description"`
}

type Job struct {
	ID            int        `json:"id"`
	Name          string     `json:"name"`
	Stage         string     `json:"stage"`
	Status        string     `json:"status"`
	AllowFailure  bool       `json:"allow_failure"`
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	Duration      float64    `json:"duration"`
	WebURL        string     `json:"web_url"`
	Runner        *Runner    `json:"runner"`
	FailureReason string     `json:"failure_reason"`
	Pipeline      *Pipeline  `json:"pipeline"`

	// Bridges (trigger jobs) only.
	DownstreamPipeline *Pipeline `json:"downstream_pipeline"`
	IsBridge           bool      `json:"is_bridge"`
}

type MR struct {
	ID                  int       `json:"id"`
	IID                 int       `json:"iid"`
	ProjectID           int       `json:"project_id"`
	Title               string    `json:"title"`
	Description         string    `json:"description"`
	State               string    `json:"state"`
	Draft               bool      `json:"draft"`
	SourceBranch        string    `json:"source_branch"`
	TargetBranch        string    `json:"target_branch"`
	WebURL              string    `json:"web_url"`
	Author              User      `json:"author"`
	Assignees           []User    `json:"assignees"`
	Reviewers           []User    `json:"reviewers"`
	Labels              []string  `json:"labels"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	DetailedMergeStatus string    `json:"detailed_merge_status"`
	HasConflicts        bool      `json:"has_conflicts"`
	HeadPipeline        *Pipeline `json:"head_pipeline"`
	SHA                 string    `json:"sha"`
	UserNotesCount      int       `json:"user_notes_count"`
	ChangesCount        string    `json:"changes_count"`
	DivergedCommits     int       `json:"diverged_commits_count"`
	AutoMerge           bool      `json:"merge_when_pipeline_succeeds"`
	RemoveSourceBranch  bool      `json:"force_remove_source_branch"`
	Squash              bool      `json:"squash"`
}

// MRSummary is a lightweight row for the MR list (fetched via GraphQL so that
// pipeline status comes back in the same round trip).
type MRSummary struct {
	IID            int       `json:"iid"`
	Title          string    `json:"title"`
	Draft          bool      `json:"draft"`
	WebURL         string    `json:"web_url"`
	SourceBranch   string    `json:"source_branch"`
	TargetBranch   string    `json:"target_branch"`
	UpdatedAt      time.Time `json:"updated_at"`
	Author         string    `json:"author"`
	PipelineID     int       `json:"pipeline_id"`
	PipelineStatus string    `json:"pipeline_status"`
	Approved       bool      `json:"approved"`
	MergeStatus    string    `json:"merge_status"`
	Notes          int       `json:"notes"`
	Conflicts      bool      `json:"conflicts"`
	Project        string    `json:"project,omitempty"` // full path; set for cross-project lists
	State          string    `json:"state,omitempty"`   // opened, merged, closed, locked
	ClosedAt       time.Time `json:"closed_at"`         // when merged or closed
}

type Approvals struct {
	Approved          bool `json:"approved"`
	ApprovalsRequired int  `json:"approvals_required"`
	ApprovalsLeft     int  `json:"approvals_left"`
	UserHasApproved   bool `json:"user_has_approved"`
	UserCanApprove    bool `json:"user_can_approve"`
	ApprovedBy        []struct {
		User User `json:"user"`
	} `json:"approved_by"`
}

type Note struct {
	ID         int       `json:"id"`
	Body       string    `json:"body"`
	Author     User      `json:"author"`
	CreatedAt  time.Time `json:"created_at"`
	System     bool      `json:"system"`
	Resolvable bool      `json:"resolvable"`
	Resolved   bool      `json:"resolved"`
	Position   *struct {
		NewPath string `json:"new_path"`
		NewLine int    `json:"new_line"`
		OldPath string `json:"old_path"`
		OldLine int    `json:"old_line"`
	} `json:"position"`
}

type Discussion struct {
	ID    string `json:"id"`
	Notes []Note `json:"notes"`
}

// ---- reads ----

func (c *Client) CurrentUser(ctx context.Context) (*User, error) {
	var u User
	return &u, c.get(ctx, "/user", nil, &u)
}

type MRFilter string

const (
	MRAll      MRFilter = "all"
	MRMine     MRFilter = "mine"
	MRReviewer MRFilter = "review"
	MRMerged   MRFilter = "merged"
	MRClosed   MRFilter = "closed"
)

const mrFields = `nodes { iid title draft webUrl sourceBranch targetBranch updatedAt
  author { username } headPipeline { id status } approved detailedMergeStatus
  userNotesCount conflicts project { fullPath } state mergedAt closedAt }`

type gqlMRConn struct {
	Nodes []struct {
		IID          string    `json:"iid"`
		Title        string    `json:"title"`
		Draft        bool      `json:"draft"`
		WebURL       string    `json:"webUrl"`
		SourceBranch string    `json:"sourceBranch"`
		TargetBranch string    `json:"targetBranch"`
		UpdatedAt    time.Time `json:"updatedAt"`
		Author       *struct {
			Username string `json:"username"`
		} `json:"author"`
		HeadPipeline *struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"headPipeline"`
		Approved            bool   `json:"approved"`
		DetailedMergeStatus string `json:"detailedMergeStatus"`
		UserNotesCount      int    `json:"userNotesCount"`
		Conflicts           bool   `json:"conflicts"`
		Project             *struct {
			FullPath string `json:"fullPath"`
		} `json:"project"`
		State    string     `json:"state"`
		MergedAt *time.Time `json:"mergedAt"`
		ClosedAt *time.Time `json:"closedAt"`
	} `json:"nodes"`
}

func (conn gqlMRConn) summaries() []MRSummary {
	res := make([]MRSummary, 0, len(conn.Nodes))
	for _, n := range conn.Nodes {
		s := MRSummary{
			Title: n.Title, Draft: n.Draft, WebURL: n.WebURL,
			SourceBranch: n.SourceBranch, TargetBranch: n.TargetBranch,
			UpdatedAt: n.UpdatedAt, Approved: n.Approved,
			MergeStatus: strings.ToLower(n.DetailedMergeStatus),
			Notes:       n.UserNotesCount, Conflicts: n.Conflicts,
			State: n.State,
		}
		switch {
		case n.MergedAt != nil:
			s.ClosedAt = *n.MergedAt
		case n.ClosedAt != nil:
			s.ClosedAt = *n.ClosedAt
		}
		s.IID, _ = strconv.Atoi(n.IID)
		if n.Author != nil {
			s.Author = n.Author.Username
		}
		if n.HeadPipeline != nil {
			s.PipelineID = gidInt(n.HeadPipeline.ID)
			s.PipelineStatus = strings.ToLower(n.HeadPipeline.Status)
		}
		if n.Project != nil {
			s.Project = n.Project.FullPath
		}
		res = append(res, s)
	}
	return res
}

func gidInt(gid string) int {
	n, _ := strconv.Atoi(gid[strings.LastIndex(gid, "/")+1:])
	return n
}

func (c *Client) ListMRs(ctx context.Context, project string, filter MRFilter) ([]MRSummary, error) {
	var q string
	switch filter {
	case MRMine:
		q = `query($p:String!){ currentUser { authoredMergeRequests(projectPath:$p, state:opened, first:100, sort:UPDATED_DESC) { ` + mrFields + ` } } }`
	case MRReviewer:
		q = `query($p:String!){ currentUser { reviewRequestedMergeRequests(projectPath:$p, state:opened, first:100, sort:UPDATED_DESC) { ` + mrFields + ` } } }`
	case MRMerged:
		q = `query($p:ID!){ project(fullPath:$p) { mergeRequests(state:merged, first:100, sort:MERGED_AT_DESC) { ` + mrFields + ` } } }`
	case MRClosed:
		q = `query($p:ID!){ project(fullPath:$p) { mergeRequests(state:closed, first:100, sort:UPDATED_DESC) { ` + mrFields + ` } } }`
	default:
		q = `query($p:ID!){ project(fullPath:$p) { mergeRequests(state:opened, first:100, sort:UPDATED_DESC) { ` + mrFields + ` } } }`
	}
	var out struct {
		Project *struct {
			MergeRequests gqlMRConn `json:"mergeRequests"`
		} `json:"project"`
		CurrentUser *struct {
			Authored gqlMRConn `json:"authoredMergeRequests"`
			Review   gqlMRConn `json:"reviewRequestedMergeRequests"`
		} `json:"currentUser"`
	}
	if err := c.graphql(ctx, q, map[string]any{"p": project}, &out); err != nil {
		return nil, err
	}
	var conn gqlMRConn
	switch {
	case filter == MRMine && out.CurrentUser != nil:
		conn = out.CurrentUser.Authored
	case filter == MRReviewer && out.CurrentUser != nil:
		conn = out.CurrentUser.Review
	case out.Project != nil:
		conn = out.Project.MergeRequests
	default:
		return nil, fmt.Errorf("project %s not found", project)
	}
	return conn.summaries(), nil
}

func (c *Client) GetMR(ctx context.Context, project string, iid int) (*MR, error) {
	var mr MR
	q := url.Values{"include_diverged_commits_count": {"true"}}
	return &mr, c.get(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d", pid(project), iid), q, &mr)
}

// FindMRForBranch returns the open MR whose source branch is branch, or nil.
func (c *Client) FindMRForBranch(ctx context.Context, project, branch string) (*MR, error) {
	var mrs []MR
	q := url.Values{"source_branch": {branch}, "state": {"opened"}, "per_page": {"1"}}
	if err := c.get(ctx, fmt.Sprintf("/projects/%s/merge_requests", pid(project)), q, &mrs); err != nil {
		return nil, err
	}
	if len(mrs) == 0 {
		return nil, nil
	}
	return &mrs[0], nil
}

func (c *Client) GetApprovals(ctx context.Context, project string, iid int) (*Approvals, error) {
	var a Approvals
	return &a, c.get(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d/approvals", pid(project), iid), nil, &a)
}

func (c *Client) ListDiscussions(ctx context.Context, project string, iid int) ([]Discussion, error) {
	return getAll[Discussion](ctx, c, fmt.Sprintf("/projects/%s/merge_requests/%d/discussions", pid(project), iid), nil, 5)
}

func (c *Client) ListPipelines(ctx context.Context, project, ref string) ([]Pipeline, error) {
	q := url.Values{"per_page": {"50"}}
	if ref != "" {
		q.Set("ref", ref)
	}
	var ps []Pipeline
	return ps, c.get(ctx, fmt.Sprintf("/projects/%s/pipelines", pid(project)), q, &ps)
}

func (c *Client) GetPipeline(ctx context.Context, project string, id int) (*Pipeline, error) {
	var p Pipeline
	return &p, c.get(ctx, fmt.Sprintf("/projects/%s/pipelines/%d", pid(project), id), nil, &p)
}

// ListPipelineJobs returns the pipeline's jobs and bridges (trigger jobs).
func (c *Client) ListPipelineJobs(ctx context.Context, project string, id int) ([]Job, error) {
	type res struct {
		jobs []Job
		err  error
	}
	bch := make(chan res, 1)
	go func() {
		b, err := getAll[Job](ctx, c, fmt.Sprintf("/projects/%s/pipelines/%d/bridges", pid(project), id), nil, 3)
		bch <- res{b, err}
	}()
	jobs, err := getAll[Job](ctx, c, fmt.Sprintf("/projects/%s/pipelines/%d/jobs", pid(project), id), nil, 10)
	if err != nil {
		return nil, err
	}
	if b := <-bch; b.err == nil {
		for i := range b.jobs {
			b.jobs[i].IsBridge = true
		}
		jobs = append(jobs, b.jobs...)
	}
	return jobs, nil
}

func (c *Client) GetJob(ctx context.Context, project string, id int) (*Job, error) {
	var j Job
	return &j, c.get(ctx, fmt.Sprintf("/projects/%s/jobs/%d", pid(project), id), nil, &j)
}

// TraceChunk fetches the job log starting at byte offset. If the server
// honours the Range request, Partial is true and Data holds only the new
// bytes; otherwise Data is the whole log.
type TraceChunk struct {
	Data    []byte
	Partial bool
}

func (c *Client) Trace(ctx context.Context, project string, jobID int, offset int) (*TraceChunk, error) {
	req, err := c.newRequest(ctx, http.MethodGet, fmt.Sprintf("/projects/%s/jobs/%d/trace", pid(project), jobID), nil, nil)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return &TraceChunk{Partial: true}, nil // nothing new yet
	}
	if resp.StatusCode >= 300 {
		return nil, readError(resp)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &TraceChunk{Data: b, Partial: resp.StatusCode == http.StatusPartialContent}, nil
}

// DashboardMRs returns the user's open MRs and MRs awaiting their review,
// across all projects, in one round trip.
func (c *Client) DashboardMRs(ctx context.Context) (*Dashboard, error) {
	q := `query { currentUser { username
	  authoredMergeRequests(state:opened, first:50, sort:UPDATED_DESC) { ` + mrFields + ` }
	  reviewRequestedMergeRequests(state:opened, first:50, sort:UPDATED_DESC) { ` + mrFields + ` } } }`
	var out struct {
		CurrentUser *struct {
			Username string    `json:"username"`
			Authored gqlMRConn `json:"authoredMergeRequests"`
			Review   gqlMRConn `json:"reviewRequestedMergeRequests"`
		} `json:"currentUser"`
	}
	if err := c.graphql(ctx, q, nil, &out); err != nil {
		return nil, err
	}
	if out.CurrentUser == nil {
		return nil, fmt.Errorf("not authenticated")
	}
	return &Dashboard{
		Username: out.CurrentUser.Username,
		Authored: out.CurrentUser.Authored.summaries(),
		Review:   out.CurrentUser.Review.summaries(),
	}, nil
}

type Dashboard struct {
	Username string      `json:"username"`
	Authored []MRSummary `json:"authored"`
	Review   []MRSummary `json:"review"`
}

type Project struct {
	ID                int       `json:"id"`
	Name              string    `json:"name"`
	PathWithNamespace string    `json:"path_with_namespace"`
	Description       string    `json:"description"`
	DefaultBranch     string    `json:"default_branch"`
	WebURL            string    `json:"web_url"`
	LastActivityAt    time.Time `json:"last_activity_at"`
	StarCount         int       `json:"star_count"`
	ForksCount        int       `json:"forks_count"`
	Topics            []string  `json:"topics"`
	Archived          bool      `json:"archived"`
}

// ListProjects returns projects the user is a member of, most recently
// active first.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	q := url.Values{"membership": {"true"}, "simple": {"true"}, "order_by": {"last_activity_at"}, "archived": {"false"}}
	return getAll[Project](ctx, c, "/projects", q, 20)
}

// SearchProjects searches all projects visible to the user.
func (c *Client) SearchProjects(ctx context.Context, term string) ([]Project, error) {
	q := url.Values{"search": {term}, "simple": {"true"}, "order_by": {"last_activity_at"}, "per_page": {"50"}, "search_namespaces": {"true"}}
	var ps []Project
	return ps, c.get(ctx, "/projects", q, &ps)
}

// OpenMRCount returns the number of open MRs in a project.
func (c *Client) OpenMRCount(ctx context.Context, project string) (int, error) {
	q := url.Values{"state": {"opened"}, "per_page": {"1"}}
	h, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/projects/%s/merge_requests", pid(project)), q, nil, nil)
	if err != nil {
		return 0, err
	}
	n, _ := strconv.Atoi(h.Get("X-Total"))
	return n, nil
}

type Todo struct {
	ID         int       `json:"id"`
	ActionName string    `json:"action_name"`
	TargetType string    `json:"target_type"`
	TargetURL  string    `json:"target_url"`
	Body       string    `json:"body"`
	State      string    `json:"state"`
	CreatedAt  time.Time `json:"created_at"`
	Author     User      `json:"author"`
	Project    *struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
	Target struct {
		IID   int    `json:"iid"`
		Title string `json:"title"`
	} `json:"target"`
}

func (c *Client) ListTodos(ctx context.Context) ([]Todo, error) {
	q := url.Values{"state": {"pending"}, "per_page": {"50"}}
	var ts []Todo
	return ts, c.get(ctx, "/todos", q, &ts)
}

// ---- writes ----

func (c *Client) MarkTodoDone(ctx context.Context, id int) error {
	return c.post(ctx, fmt.Sprintf("/todos/%d/mark_as_done", id), nil, nil)
}

func (c *Client) MarkAllTodosDone(ctx context.Context) error {
	return c.post(ctx, "/todos/mark_as_done", nil, nil)
}

func (c *Client) post(ctx context.Context, path string, q url.Values, out any) error {
	_, err := c.do(ctx, http.MethodPost, path, q, nil, out)
	return err
}

func (c *Client) RetryJob(ctx context.Context, project string, id int) (*Job, error) {
	var j Job
	return &j, c.post(ctx, fmt.Sprintf("/projects/%s/jobs/%d/retry", pid(project), id), nil, &j)
}

func (c *Client) PlayJob(ctx context.Context, project string, id int) (*Job, error) {
	var j Job
	return &j, c.post(ctx, fmt.Sprintf("/projects/%s/jobs/%d/play", pid(project), id), nil, &j)
}

func (c *Client) CancelJob(ctx context.Context, project string, id int) error {
	return c.post(ctx, fmt.Sprintf("/projects/%s/jobs/%d/cancel", pid(project), id), nil, nil)
}

func (c *Client) RetryPipeline(ctx context.Context, project string, id int) error {
	return c.post(ctx, fmt.Sprintf("/projects/%s/pipelines/%d/retry", pid(project), id), nil, nil)
}

func (c *Client) CancelPipeline(ctx context.Context, project string, id int) error {
	return c.post(ctx, fmt.Sprintf("/projects/%s/pipelines/%d/cancel", pid(project), id), nil, nil)
}

func (c *Client) CreatePipeline(ctx context.Context, project, ref string) (*Pipeline, error) {
	var p Pipeline
	return &p, c.post(ctx, fmt.Sprintf("/projects/%s/pipeline", pid(project)), url.Values{"ref": {ref}}, &p)
}

func (c *Client) Approve(ctx context.Context, project string, iid int) error {
	return c.post(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d/approve", pid(project), iid), nil, nil)
}

func (c *Client) Unapprove(ctx context.Context, project string, iid int) error {
	return c.post(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d/unapprove", pid(project), iid), nil, nil)
}

// Merge merges the MR now, or sets it to auto-merge when the pipeline
// succeeds if auto is true.
func (c *Client) Merge(ctx context.Context, project string, iid int, auto bool) error {
	q := url.Values{}
	if auto {
		q.Set("merge_when_pipeline_succeeds", "true")
		q.Set("auto_merge", "true")
	}
	_, err := c.do(ctx, http.MethodPut, fmt.Sprintf("/projects/%s/merge_requests/%d/merge", pid(project), iid), q, nil, nil)
	return err
}

func (c *Client) Rebase(ctx context.Context, project string, iid int) error {
	_, err := c.do(ctx, http.MethodPut, fmt.Sprintf("/projects/%s/merge_requests/%d/rebase", pid(project), iid), nil, nil, nil)
	return err
}

func (c *Client) SetTitle(ctx context.Context, project string, iid int, title string) error {
	_, err := c.do(ctx, http.MethodPut, fmt.Sprintf("/projects/%s/merge_requests/%d", pid(project), iid), nil, map[string]string{"title": title}, nil)
	return err
}

// UpdateMR changes the given fields of an MR (title, description,
// target_branch, labels, assignee_ids, reviewer_ids, ...).
func (c *Client) UpdateMR(ctx context.Context, project string, iid int, fields map[string]any) (*MR, error) {
	var mr MR
	_, err := c.do(ctx, http.MethodPut, fmt.Sprintf("/projects/%s/merge_requests/%d", pid(project), iid), nil, fields, &mr)
	return &mr, err
}

func (c *Client) AddNote(ctx context.Context, project string, iid int, body string) error {
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/projects/%s/merge_requests/%d/notes", pid(project), iid), nil, map[string]string{"body": body}, nil)
	return err
}

type Label struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	TextColor   string `json:"text_color"`
	Description string `json:"description"`
}

// ListLabels lists the labels usable in a project, including group labels.
func (c *Client) ListLabels(ctx context.Context, project string) ([]Label, error) {
	return getAll[Label](ctx, c, fmt.Sprintf("/projects/%s/labels", pid(project)),
		url.Values{"include_ancestor_groups": {"true"}}, 20)
}

// UpdateLabels adds and removes MR labels, leaving any others alone.
func (c *Client) UpdateLabels(ctx context.Context, project string, iid int, add, remove []string) error {
	body := map[string]string{}
	if len(add) > 0 {
		body["add_labels"] = strings.Join(add, ",")
	}
	if len(remove) > 0 {
		body["remove_labels"] = strings.Join(remove, ",")
	}
	_, err := c.do(ctx, http.MethodPut, fmt.Sprintf("/projects/%s/merge_requests/%d", pid(project), iid), nil, body, nil)
	return err
}

// CreateMRPipeline runs a merge request pipeline (needs CI rules for
// merge_request_event).
func (c *Client) CreateMRPipeline(ctx context.Context, project string, iid int) (*Pipeline, error) {
	var p Pipeline
	return &p, c.post(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d/pipelines", pid(project), iid), nil, &p)
}

// ProjectInfo holds the project settings needed to open a merge request.
type ProjectInfo struct {
	ID                int        `json:"id"`
	Name              string     `json:"name"`
	PathWithNamespace string     `json:"path_with_namespace"`
	Description       string     `json:"description"`
	WebURL            string     `json:"web_url"`
	SSHURL            string     `json:"ssh_url_to_repo"`
	Visibility        string     `json:"visibility"`
	CreatedAt         time.Time  `json:"created_at"`
	LastActivityAt    time.Time  `json:"last_activity_at"`
	StarCount         int        `json:"star_count"`
	ForksCount        int        `json:"forks_count"`
	OpenIssuesCount   int        `json:"open_issues_count"`
	Topics            []string   `json:"topics"`
	Archived          bool       `json:"archived"`
	EmptyRepo         bool       `json:"empty_repo"`
	Statistics        *RepoStats `json:"statistics"`
	Namespace         struct {
		FullPath string `json:"full_path"`
	} `json:"namespace"`
	DefaultBranch        string `json:"default_branch"`
	RemoveSourceBranch   bool   `json:"remove_source_branch_after_merge"`
	SquashOption         string `json:"squash_option"`
	MergeRequestTemplate string `json:"merge_requests_template"`
}

type RepoStats struct {
	CommitCount    int   `json:"commit_count"`
	RepositorySize int64 `json:"repository_size"`
}

func (c *Client) GetProject(ctx context.Context, project string) (*ProjectInfo, error) {
	var p ProjectInfo
	// statistics needs Reporter access; GitLab just omits it otherwise
	return &p, c.get(ctx, fmt.Sprintf("/projects/%s", pid(project)), url.Values{"statistics": {"true"}}, &p)
}

type Tag struct {
	Name      string     `json:"name"`
	Message   string     `json:"message"`
	Target    string     `json:"target"`
	Protected bool       `json:"protected"`
	CreatedAt *time.Time `json:"created_at"`
	Commit    Commit     `json:"commit"`
	Release   *struct {
		TagName     string `json:"tag_name"`
		Description string `json:"description"`
	} `json:"release"`
}

// When is the tag's creation time, falling back to its commit's date for
// lightweight tags.
func (t Tag) When() time.Time {
	if t.CreatedAt != nil {
		return *t.CreatedAt
	}
	return t.Commit.CommittedDate
}

// ListTags lists tags, most recently updated first.
func (c *Client) ListTags(ctx context.Context, project string) ([]Tag, error) {
	return getAll[Tag](ctx, c, fmt.Sprintf("/projects/%s/repository/tags", pid(project)),
		url.Values{"order_by": {"updated"}, "sort": {"desc"}}, 5)
}

type Commit struct {
	ID            string    `json:"id"`
	ShortID       string    `json:"short_id"`
	Title         string    `json:"title"`
	Message       string    `json:"message"`
	CommittedDate time.Time `json:"committed_date"`
}

type Branch struct {
	Name      string `json:"name"`
	Default   bool   `json:"default"`
	Protected bool   `json:"protected"`
	Commit    Commit `json:"commit"`
}

// ListBranches lists branches, most recently updated first.
func (c *Client) ListBranches(ctx context.Context, project string) ([]Branch, error) {
	return getAll[Branch](ctx, c, fmt.Sprintf("/projects/%s/repository/branches", pid(project)),
		url.Values{"sort": {"updated_desc"}}, 10)
}

// Compare lists the commits on to that aren't on from (from the merge base).
func (c *Client) Compare(ctx context.Context, project, from, to string) ([]Commit, error) {
	var out struct {
		Commits []Commit `json:"commits"`
	}
	q := url.Values{"from": {from}, "to": {to}}
	return out.Commits, c.get(ctx, fmt.Sprintf("/projects/%s/repository/compare", pid(project)), q, &out)
}

type Template struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ListMRTemplates lists the merge request description templates available
// to a project (its own and any inherited ones).
func (c *Client) ListMRTemplates(ctx context.Context, project string) ([]Template, error) {
	return getAll[Template](ctx, c, fmt.Sprintf("/projects/%s/templates/merge_requests", pid(project)), nil, 5)
}

func (c *Client) GetMRTemplate(ctx context.Context, project, key string) (string, error) {
	var t Template
	err := c.get(ctx, fmt.Sprintf("/projects/%s/templates/merge_requests/%s", pid(project), url.PathEscape(key)), nil, &t)
	return t.Content, err
}

// ListMembers lists users with access to the project, including inherited
// members.
func (c *Client) ListMembers(ctx context.Context, project string) ([]User, error) {
	return getAll[User](ctx, c, fmt.Sprintf("/projects/%s/members/all", pid(project)),
		url.Values{"state": {"active"}}, 5)
}

type NewMR struct {
	SourceBranch       string `json:"source_branch"`
	TargetBranch       string `json:"target_branch"`
	Title              string `json:"title"`
	Description        string `json:"description,omitempty"`
	Labels             string `json:"labels,omitempty"`
	AssigneeIDs        []int  `json:"assignee_ids,omitempty"`
	ReviewerIDs        []int  `json:"reviewer_ids,omitempty"`
	RemoveSourceBranch bool   `json:"remove_source_branch"`
	Squash             bool   `json:"squash"`
}

func (c *Client) CreateMR(ctx context.Context, project string, m NewMR) (*MR, error) {
	var mr MR
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/projects/%s/merge_requests", pid(project)), nil, m, &mr)
	return &mr, err
}

// TestCase is one test from a pipeline's JUnit reports.
type TestCase struct {
	Status        string  `json:"status"` // success, failed, skipped, error
	Name          string  `json:"name"`
	Classname     string  `json:"classname"`
	File          string  `json:"file"`
	ExecutionTime float64 `json:"execution_time"`
	SystemOutput  string  `json:"system_output"`
	StackTrace    string  `json:"stack_trace"`
}

// TestSuite is one job's (or one matrix job's) tests.
type TestSuite struct {
	Name         string     `json:"name"`
	TotalTime    float64    `json:"total_time"`
	TotalCount   int        `json:"total_count"`
	SuccessCount int        `json:"success_count"`
	FailedCount  int        `json:"failed_count"`
	SkippedCount int        `json:"skipped_count"`
	ErrorCount   int        `json:"error_count"`
	SuiteError   string     `json:"suite_error"`
	BuildIDs     []int      `json:"build_ids"`
	TestCases    []TestCase `json:"test_cases"`
}

// TestReport is a pipeline's test results, as on its Tests tab.
type TestReport struct {
	TotalTime    float64     `json:"total_time"`
	TotalCount   int         `json:"total_count"`
	SuccessCount int         `json:"success_count"`
	FailedCount  int         `json:"failed_count"`
	SkippedCount int         `json:"skipped_count"`
	ErrorCount   int         `json:"error_count"`
	TestSuites   []TestSuite `json:"test_suites"`
}

// TestSummary is the counts alone, without the test cases.
type TestSummary struct {
	Total struct {
		Time    float64 `json:"time"`
		Count   int     `json:"count"`
		Success int     `json:"success"`
		Failed  int     `json:"failed"`
		Skipped int     `json:"skipped"`
		Error   int     `json:"error"`
	} `json:"total"`
	// the suites' counts and jobs (the full report leaves the jobs out)
	TestSuites []TestSuite `json:"test_suites"`
}

func (c *Client) TestReportSummary(ctx context.Context, project string, pipeline int) (*TestSummary, error) {
	var s TestSummary
	return &s, c.get(ctx, fmt.Sprintf("/projects/%s/pipelines/%d/test_report_summary", pid(project), pipeline), nil, &s)
}

func (c *Client) TestReport(ctx context.Context, project string, pipeline int) (*TestReport, error) {
	var r TestReport
	return &r, c.get(ctx, fmt.Sprintf("/projects/%s/pipelines/%d/test_report", pid(project), pipeline), nil, &r)
}

// JobNeeds maps each of a pipeline's jobs to the jobs it waits for: its
// needs, or for jobs without needs, the jobs of the stage before. Matrix
// jobs come back individually. project is a path or a numeric ID.
func (c *Client) JobNeeds(ctx context.Context, project string, pipelineIID int) (map[string][]string, error) {
	const fields = `pipeline(iid: $iid) { jobs(first: 100, after: $after, retried: false) {
		pageInfo { hasNextPage endCursor }
		nodes { name previousStageJobsOrNeeds { nodes { ... on CiBuildNeed { name } ... on CiJob { name } } } } } }`
	q := `query($p: ID!, $iid: ID!, $after: String) { project(fullPath: $p) { ` + fields + ` } }`
	vars := map[string]any{"p": project, "iid": strconv.Itoa(pipelineIID)}
	if _, err := strconv.Atoi(project); err == nil {
		q = `query($p: [ID!], $iid: ID!, $after: String) { projects(ids: $p) { nodes { ` + fields + ` } } }`
		vars["p"] = []string{"gid://gitlab/Project/" + project}
	}
	type page struct {
		Pipeline *struct {
			Jobs struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []struct {
					Name  string `json:"name"`
					Needs struct {
						Nodes []struct {
							Name string `json:"name"`
						} `json:"nodes"`
					} `json:"previousStageJobsOrNeeds"`
				} `json:"nodes"`
			} `json:"jobs"`
		} `json:"pipeline"`
	}
	needs := map[string][]string{}
	for {
		var out struct {
			Project  *page `json:"project"`
			Projects struct {
				Nodes []page `json:"nodes"`
			} `json:"projects"`
		}
		if err := c.graphql(ctx, q, vars, &out); err != nil {
			return nil, err
		}
		pg := out.Project
		if pg == nil && len(out.Projects.Nodes) > 0 {
			pg = &out.Projects.Nodes[0]
		}
		if pg == nil || pg.Pipeline == nil {
			return nil, fmt.Errorf("pipeline not found")
		}
		jobs := pg.Pipeline.Jobs
		for _, n := range jobs.Nodes {
			deps := []string{}
			for _, d := range n.Needs.Nodes {
				deps = append(deps, d.Name)
			}
			needs[n.Name] = deps
		}
		if !jobs.PageInfo.HasNextPage {
			return needs, nil
		}
		vars["after"] = jobs.PageInfo.EndCursor
	}
}
