package confluence

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"strings"
	"text/template"
	"time"
)

type Writer struct {
	client       ConfluenceClient
	spaceKey     string
	parentPageID string
}

type WriterConfig struct {
	Client       ConfluenceClient
	SpaceKey     string
	ParentPageID string
}

func NewWriter(cfg WriterConfig) *Writer {
	return &Writer{
		client:       cfg.Client,
		spaceKey:     cfg.SpaceKey,
		parentPageID: cfg.ParentPageID,
	}
}

func (w *Writer) CreateReport(ctx context.Context, run RunReport) (*Page, error) {
	if err := run.Validate(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}

	body := RenderReportTemplate(run)
	title := GenerateReportTitle(run)

	page, err := w.client.CreatePage(ctx, CreatePageRequest{
		SpaceKey: w.spaceKey,
		Title:    title,
		Body:     body,
		ParentID: w.parentPageID,
	})
	if err != nil {
		return nil, fmt.Errorf("create page: %w", err)
	}

	return page, nil
}

func GenerateReportTitle(run RunReport) string {
	// Include time-of-day so two runs on the same task same day don't collide
	// (Confluence rejects duplicate titles within a space).
	stamp := run.StartedAt
	if stamp.IsZero() {
		stamp = time.Now()
	}
	shortRun := run.RunID[:min(8, len(run.RunID))]
	if run.IssueKey == "" {
		return fmt.Sprintf("Run %s — %s", shortRun, stamp.Format("2006-01-02 15:04:05"))
	}
	return fmt.Sprintf("%s — Run %s — %s", run.IssueKey, shortRun, stamp.Format("2006-01-02 15:04:05"))
}

const reportTemplate = `<h1>Agent Run Report: {{.IssueKey}}</h1>

<ac:structured-macro ac:name="info">
<ac:rich-text-body>
<p><strong>Agent:</strong> {{.AgentName}} | <strong>Duration:</strong> {{.DurationStr}} | <strong>Cost:</strong> ${{printf "%.2f" .CostUSD}} | <strong>Status:</strong> {{.Status}}</p>
</ac:rich-text-body>
</ac:structured-macro>

{{if .Summary}}
<h2>Summary</h2>
<p>{{.SummaryEscaped}}</p>
{{end}}

{{if .FilesChanged}}
<h2>Files Changed</h2>
<ul>
{{range .FilesChanged}}<li><code>{{.}}</code></li>
{{end}}</ul>
{{end}}

{{if .Decisions}}
<h2>Decisions Made</h2>
<ul>
{{range .Decisions}}<li>{{.}}</li>
{{end}}</ul>
{{end}}

{{if .GitDiff}}
<h2>Git Diff</h2>
<ac:structured-macro ac:name="code">
<ac:parameter ac:name="language">diff</ac:parameter>
<ac:plain-text-body><![CDATA[{{.GitDiff}}]]></ac:plain-text-body>
</ac:structured-macro>
{{end}}

<h2>Run Metadata</h2>
<table>
<tr><th>Field</th><th>Value</th></tr>
<tr><td>Run ID</td><td>{{.RunID}}</td></tr>
<tr><td>Tokens (in/out)</td><td>{{.InputTokens}} / {{.OutputTokens}}</td></tr>
<tr><td>Model</td><td>{{.Model}}</td></tr>
<tr><td>Git Before</td><td>{{.GitHeadBefore}}</td></tr>
<tr><td>Git After</td><td>{{.GitHeadAfter}}</td></tr>
<tr><td>Started</td><td>{{.StartedAtStr}}</td></tr>
<tr><td>Ended</td><td>{{.EndedAtStr}}</td></tr>
</table>`

type reportData struct {
	RunReport
	DurationStr    string
	SummaryEscaped string
	StartedAtStr   string
	EndedAtStr     string
}

func RenderReportTemplate(run RunReport) string {
	tmpl, err := template.New("report").Parse(reportTemplate)
	if err != nil {
		return fmt.Sprintf("<p>Template error: %v</p>", err)
	}

	data := reportData{
		RunReport:      run,
		DurationStr:    formatDuration(run.Duration),
		SummaryEscaped: html.EscapeString(run.Summary),
		StartedAtStr:   formatTime(run.StartedAt),
		EndedAtStr:     formatTime(run.EndedAt),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Sprintf("<p>Render error: %v</p>", err)
	}

	// Clean up empty sections
	result := buf.String()
	result = strings.ReplaceAll(result, "\n\n\n", "\n\n")

	return result
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "N/A"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Format("2006-01-02 15:04:05")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
