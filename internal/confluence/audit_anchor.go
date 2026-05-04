// Package confluence — audit_anchor.go: append-only Confluence page that
// witnesses the audit_log hash-chain tip over time.
//
// The page lives at a fixed title (e.g. "Dandori Audit Anchors"). Each
// time UpsertAuditAnchorPage is called, one row is appended to the table:
//
//	| anchored_at | last_audit_id | last_curr_hash |
//
// The page is the external witness. If the local audit_log is rewritten
// later, the recorded hash for a given last_audit_id will no longer
// reproduce — that's the tamper signal a verify --with-anchor run picks up.
//
// Idempotency:
//   - Look up the page by title via SearchPages. If absent, CreatePage with
//     a fresh table containing this anchor's row.
//   - If present, parse the existing storage body and append (or replace, if
//     the same last_audit_id is already there) before UpdatePage.
//
// This file deliberately stays HTML-storage-format-only (no ADF) because
// the rest of the package writes storage too — keeps the surface tight.
package confluence

import (
	"context"
	"fmt"
	"html"
	"strings"
)

// AuditAnchorClient is the subset of the Confluence client we need. Defined
// here (rather than reusing *Client directly) so tests can inject a fake.
type AuditAnchorClient interface {
	SearchPages(ctx context.Context, spaceKey, title string) ([]Page, error)
	CreatePage(ctx context.Context, req CreatePageRequest) (*Page, error)
	UpdatePage(ctx context.Context, pageID string, req UpdatePageRequest) (*Page, error)
}

// AuditAnchorRow is one row in the witness table.
type AuditAnchorRow struct {
	AnchoredAt   string
	LastAuditID  int64
	LastCurrHash string
}

// AuditAnchorPageTitle is the conventional title for the witness page.
// Callers may override via UpsertAuditAnchorPage's title arg if they run
// per-host or per-environment anchor pages.
const AuditAnchorPageTitle = "Dandori Audit Anchors"

const auditAnchorTableHeader = "<table><thead><tr>" +
	"<th>anchored_at</th>" +
	"<th>last_audit_id</th>" +
	"<th>last_curr_hash</th>" +
	"</tr></thead><tbody>"

const auditAnchorTableFooter = "</tbody></table>"

// renderAuditAnchorBody renders the full storage-format body for the page,
// given a complete (deduped, sorted) row set.
func renderAuditAnchorBody(rows []AuditAnchorRow) string {
	var b strings.Builder
	b.WriteString("<p>Append-only witness of the local audit_log hash-chain tip. ")
	b.WriteString("Every row was written by <code>dandori audit anchor</code>. ")
	b.WriteString("Verify with <code>dandori audit verify --with-anchor</code>.</p>")
	b.WriteString(auditAnchorTableHeader)
	for _, r := range rows {
		b.WriteString("<tr><td>")
		b.WriteString(html.EscapeString(r.AnchoredAt))
		b.WriteString("</td><td>")
		b.WriteString(fmt.Sprintf("%d", r.LastAuditID))
		b.WriteString("</td><td><code>")
		b.WriteString(html.EscapeString(r.LastCurrHash))
		b.WriteString("</code></td></tr>")
	}
	b.WriteString(auditAnchorTableFooter)
	return b.String()
}

// UpsertAuditAnchorPage finds-or-creates the witness page in spaceKey and
// appends row. If a page already exists, all previously-rendered rows are
// preserved (we re-render the full body on every upsert — Confluence storage
// format doesn't support patch updates). If a row with the same
// last_audit_id already exists on the page, the upsert is a silent no-op
// (returning the existing page unchanged) — same idempotency guarantee as
// the local audit_anchors UNIQUE(last_audit_id) constraint.
func UpsertAuditAnchorPage(ctx context.Context, c AuditAnchorClient, spaceKey, title string, row AuditAnchorRow) (*Page, error) {
	if title == "" {
		title = AuditAnchorPageTitle
	}
	if spaceKey == "" {
		return nil, fmt.Errorf("upsert audit anchor: space key required")
	}
	if row.LastCurrHash == "" {
		return nil, fmt.Errorf("upsert audit anchor: empty hash")
	}

	pages, err := c.SearchPages(ctx, spaceKey, title)
	if err != nil {
		return nil, fmt.Errorf("upsert audit anchor: search: %w", err)
	}
	var existing *Page
	for i := range pages {
		if pages[i].Title == title {
			existing = &pages[i]
			break
		}
	}

	if existing == nil {
		body := renderAuditAnchorBody([]AuditAnchorRow{row})
		page, err := c.CreatePage(ctx, CreatePageRequest{
			SpaceKey: spaceKey,
			Title:    title,
			Body:     body,
		})
		if err != nil {
			return nil, fmt.Errorf("upsert audit anchor: create: %w", err)
		}
		return page, nil
	}

	rows := parseAuditAnchorRows(existing.Body.Storage.Value)
	for _, r := range rows {
		if r.LastAuditID == row.LastAuditID {
			// Already anchored — return the existing page so the caller's
			// status writeback can still record confluence_page_id.
			return existing, nil
		}
	}
	rows = append(rows, row)
	body := renderAuditAnchorBody(rows)

	updated, err := c.UpdatePage(ctx, existing.ID, UpdatePageRequest{
		Title:   title,
		Body:    body,
		Version: PageVersion{Number: existing.Version.Number + 1},
	})
	if err != nil {
		return nil, fmt.Errorf("upsert audit anchor: update: %w", err)
	}
	return updated, nil
}

// parseAuditAnchorRows extracts existing AuditAnchorRow entries from a
// previously-rendered page body. We only parse what renderAuditAnchorBody
// emits — any other content silently round-trips as new rows are appended.
// This is intentionally permissive: a hand-edited page just gets its rows
// stripped on the next upsert, which is the desired behavior (the table
// must remain machine-trustworthy).
func parseAuditAnchorRows(body string) []AuditAnchorRow {
	out := []AuditAnchorRow{}
	// Walk <tr>...</tr> blocks inside the tbody.
	tbodyStart := strings.Index(body, "<tbody>")
	tbodyEnd := strings.Index(body, "</tbody>")
	if tbodyStart < 0 || tbodyEnd < 0 || tbodyEnd <= tbodyStart {
		return out
	}
	inner := body[tbodyStart+len("<tbody>") : tbodyEnd]
	for {
		i := strings.Index(inner, "<tr>")
		j := strings.Index(inner, "</tr>")
		if i < 0 || j < 0 || j <= i {
			break
		}
		row := inner[i+len("<tr>") : j]
		inner = inner[j+len("</tr>"):]
		cells := extractTDs(row)
		if len(cells) != 3 {
			continue
		}
		var id int64
		fmt.Sscanf(cells[1], "%d", &id)
		out = append(out, AuditAnchorRow{
			AnchoredAt:   html.UnescapeString(cells[0]),
			LastAuditID:  id,
			LastCurrHash: stripCodeTags(html.UnescapeString(cells[2])),
		})
	}
	return out
}

func extractTDs(row string) []string {
	cells := []string{}
	for {
		i := strings.Index(row, "<td>")
		j := strings.Index(row, "</td>")
		if i < 0 || j < 0 || j <= i {
			break
		}
		cells = append(cells, row[i+len("<td>"):j])
		row = row[j+len("</td>"):]
	}
	return cells
}

func stripCodeTags(s string) string {
	s = strings.TrimPrefix(s, "<code>")
	s = strings.TrimSuffix(s, "</code>")
	return s
}
