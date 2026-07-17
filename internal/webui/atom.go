package webui

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"time"

	"github.com/JetManiack/go-rss-update-handler/internal/storage"
)

// feedMaxItems bounds the Atom feed to the most recent N updates, mirroring how
// GitHub's own releases.atom serves a fixed window rather than the full history
// (verified: GitHub returns 10 entries).
const feedMaxItems = 10

// importanceScheme distinguishes the importance <category> from the classifier
// category so feed readers can filter on it.
const importanceScheme = "urn:gruh:importance"

type atomLink struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
	Href string `xml:"href,attr"`
}

type atomCategory struct {
	Term   string `xml:"term,attr"`
	Scheme string `xml:"scheme,attr,omitempty"`
}

// atomText is a text construct (e.g. <summary type="text">).
type atomText struct {
	Type string `xml:"type,attr,omitempty"`
	Body string `xml:",chardata"`
}

// atomContent is <content>; for type="html" the body is escaped markup, which
// encoding/xml produces automatically from the string field.
type atomContent struct {
	Type string `xml:"type,attr"`
	Body string `xml:",chardata"`
}

type atomEntry struct {
	ID         string         `xml:"id"`
	Title      string         `xml:"title"`
	Updated    string         `xml:"updated"`
	Published  string         `xml:"published,omitempty"`
	Links      []atomLink     `xml:"link"`
	Categories []atomCategory `xml:"category"`
	Summary    *atomText      `xml:"summary,omitempty"`
	Content    *atomContent   `xml:"content,omitempty"`
}

type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Links   []atomLink  `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

// importanceTerm maps the nullable verdict to a stable term.
func importanceTerm(important *bool) string {
	switch {
	case important == nil:
		return "pending"
	case *important:
		return "important"
	default:
		return "noise"
	}
}

// verdictSummary is the human-readable verdict line shown as the entry summary.
func verdictSummary(u storage.Update) string {
	return fmt.Sprintf("Verdict: %s (%s, confidence %.2f) — %s",
		importanceTerm(u.VerdictImportant), u.VerdictCategory, u.VerdictConfidence, u.VerdictReason)
}

// renderAtom serializes updates as an Atom 1.0 feed. selfURL is the feed's own
// address (used for <id> and rel="self"). RawContent is sanitized with the same
// policy as the JSON API before being embedded as HTML content.
func renderAtom(selfURL string, updates []storage.Update) ([]byte, error) {
	feed := atomFeed{
		Title: "go-rss-update-handler",
		ID:    selfURL,
		Links: []atomLink{
			{Rel: "self", Type: "application/atom+xml", Href: selfURL},
			{Rel: "alternate", Type: "text/html", Href: "/"},
		},
	}

	var latest time.Time
	for _, u := range updates {
		if u.PublishedAt.After(latest) {
			latest = u.PublishedAt
		}
		ts := u.PublishedAt.UTC().Format(time.RFC3339)
		entry := atomEntry{
			ID:        "urn:uuid:" + u.ID,
			Title:     u.Title,
			Updated:   ts,
			Published: ts,
			Links:     []atomLink{{Rel: "alternate", Type: "text/html", Href: u.SourceURL}},
			Summary:   &atomText{Type: "text", Body: verdictSummary(u)},
		}
		if u.VerdictCategory != "" {
			entry.Categories = append(entry.Categories, atomCategory{Term: u.VerdictCategory})
		}
		entry.Categories = append(entry.Categories, atomCategory{
			Term:   importanceTerm(u.VerdictImportant),
			Scheme: importanceScheme,
		})
		if u.RawContent != nil {
			entry.Content = &atomContent{Type: "html", Body: htmlPolicy.Sanitize(u.RawContent.Content)}
		}
		feed.Entries = append(feed.Entries, entry)
	}

	if latest.IsZero() {
		latest = time.Now()
	}
	feed.Updated = latest.UTC().Format(time.RFC3339)

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(feed); err != nil {
		return nil, fmt.Errorf("webui: encode atom feed: %w", err)
	}
	return buf.Bytes(), nil
}
