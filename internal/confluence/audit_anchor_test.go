package confluence

import (
	"context"
	"strings"
	"testing"
)

type fakeAnchorClient struct {
	pages    []Page
	created  []CreatePageRequest
	updated  map[string]UpdatePageRequest
	createID string
	createV  int
}

func (f *fakeAnchorClient) SearchPages(_ context.Context, _, title string) ([]Page, error) {
	out := []Page{}
	for _, p := range f.pages {
		if p.Title == title {
			out = append(out, p)
		}
	}
	return out, nil
}
func (f *fakeAnchorClient) CreatePage(_ context.Context, req CreatePageRequest) (*Page, error) {
	f.created = append(f.created, req)
	id := f.createID
	if id == "" {
		id = "PAGE-NEW"
	}
	v := f.createV
	if v == 0 {
		v = 1
	}
	page := Page{
		ID:      id,
		Title:   req.Title,
		Type:    "page",
		Body:    PageBody{Storage: StorageBody{Value: req.Body, Representation: "storage"}},
		Version: PageVersion{Number: v},
	}
	f.pages = append(f.pages, page)
	return &page, nil
}
func (f *fakeAnchorClient) UpdatePage(_ context.Context, pageID string, req UpdatePageRequest) (*Page, error) {
	if f.updated == nil {
		f.updated = map[string]UpdatePageRequest{}
	}
	f.updated[pageID] = req
	for i := range f.pages {
		if f.pages[i].ID == pageID {
			f.pages[i].Title = req.Title
			f.pages[i].Body = PageBody{Storage: StorageBody{Value: req.Body, Representation: "storage"}}
			f.pages[i].Version = req.Version
			out := f.pages[i]
			return &out, nil
		}
	}
	return nil, nil
}

func TestUpsertAuditAnchorPage_CreatesPageWhenMissing(t *testing.T) {
	c := &fakeAnchorClient{}
	row := AuditAnchorRow{AnchoredAt: "2026-05-04T01:00:00Z", LastAuditID: 7, LastCurrHash: "abc123"}
	page, err := UpsertAuditAnchorPage(context.Background(), c, "ENG", "Witness", row)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(c.created) != 1 {
		t.Fatalf("expected 1 create, got %d", len(c.created))
	}
	if !strings.Contains(c.created[0].Body, "abc123") {
		t.Errorf("body missing hash, got: %s", c.created[0].Body)
	}
	if page == nil || page.Title != "Witness" {
		t.Errorf("returned page wrong: %+v", page)
	}
}

func TestUpsertAuditAnchorPage_AppendsToExistingPage(t *testing.T) {
	existingBody := renderAuditAnchorBody([]AuditAnchorRow{
		{AnchoredAt: "2026-05-03T00:00:00Z", LastAuditID: 5, LastCurrHash: "older"},
	})
	c := &fakeAnchorClient{pages: []Page{{
		ID: "P1", Title: "Witness",
		Body:    PageBody{Storage: StorageBody{Value: existingBody, Representation: "storage"}},
		Version: PageVersion{Number: 4},
	}}}
	row := AuditAnchorRow{AnchoredAt: "2026-05-04T00:00:00Z", LastAuditID: 8, LastCurrHash: "newer"}
	if _, err := UpsertAuditAnchorPage(context.Background(), c, "ENG", "Witness", row); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(c.created) != 0 {
		t.Errorf("should not create when page exists")
	}
	got, ok := c.updated["P1"]
	if !ok {
		t.Fatalf("expected update on P1")
	}
	if got.Version.Number != 5 {
		t.Errorf("expected version 5, got %d", got.Version.Number)
	}
	if !strings.Contains(got.Body, "older") || !strings.Contains(got.Body, "newer") {
		t.Errorf("body must keep older + add newer, got: %s", got.Body)
	}
}

func TestUpsertAuditAnchorPage_IdempotentOnSameAuditID(t *testing.T) {
	existingBody := renderAuditAnchorBody([]AuditAnchorRow{
		{AnchoredAt: "2026-05-03T00:00:00Z", LastAuditID: 5, LastCurrHash: "h"},
	})
	c := &fakeAnchorClient{pages: []Page{{
		ID: "P1", Title: "Witness",
		Body:    PageBody{Storage: StorageBody{Value: existingBody, Representation: "storage"}},
		Version: PageVersion{Number: 1},
	}}}
	row := AuditAnchorRow{AnchoredAt: "2026-05-04T00:00:00Z", LastAuditID: 5, LastCurrHash: "h"}
	if _, err := UpsertAuditAnchorPage(context.Background(), c, "ENG", "Witness", row); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(c.updated) != 0 {
		t.Errorf("expected no update for duplicate audit id, got %d", len(c.updated))
	}
}

func TestParseAuditAnchorRows_RoundTrip(t *testing.T) {
	rows := []AuditAnchorRow{
		{AnchoredAt: "2026-05-04T01:00:00Z", LastAuditID: 1, LastCurrHash: "aa"},
		{AnchoredAt: "2026-05-04T02:00:00Z", LastAuditID: 2, LastCurrHash: "bb"},
	}
	body := renderAuditAnchorBody(rows)
	got := parseAuditAnchorRows(body)
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d (%+v)", len(got), got)
	}
	if got[0].LastAuditID != 1 || got[1].LastAuditID != 2 {
		t.Errorf("ids wrong: %+v", got)
	}
	if got[0].LastCurrHash != "aa" || got[1].LastCurrHash != "bb" {
		t.Errorf("hashes wrong: %+v", got)
	}
}

func TestUpsertAuditAnchorPage_ErrorOnEmptySpace(t *testing.T) {
	c := &fakeAnchorClient{}
	_, err := UpsertAuditAnchorPage(context.Background(), c, "", "T",
		AuditAnchorRow{LastAuditID: 1, LastCurrHash: "h"})
	if err == nil {
		t.Errorf("expected error on empty space")
	}
}
