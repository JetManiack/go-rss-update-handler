// Package webui serves a small read-only web UI that shows the updates table
// and each update's raw release text, refreshed in near real time.
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/microcosm-cc/bluemonday"

	"github.com/JetManiack/go-rss-update-handler/internal/storage"
)

//go:embed index.html go-ruh.png
var static embed.FS

// htmlPolicy sanitizes untrusted feed HTML before it is rendered in the UI:
// it keeps common formatting and strips scripts, event handlers, etc.
var htmlPolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.RequireNoFollowOnLinks(true)
	return p
}()

// Query describes a page request for the updates feed.
type Query struct {
	Limit      int
	Offset     int
	Category   string // "" = any
	Importance string // "", "important", "noise", "pending"
}

// FetchFunc returns a page of updates (with raw content) and the total number of
// matching rows.
type FetchFunc func(ctx context.Context, q Query) ([]storage.Update, int64, error)

type updateDTO struct {
	ID           string     `json:"id"`
	FeedID       string     `json:"feed_id"`
	Title        string     `json:"title"`
	SourceURL    string     `json:"source_url"`
	PublishedAt  time.Time  `json:"published_at"`
	CreatedAt    time.Time  `json:"created_at"`
	Important    *bool      `json:"important"`
	Category     string     `json:"category"`
	Confidence   float64    `json:"confidence"`
	Reason       string     `json:"reason"`
	ClassifiedAt *time.Time `json:"classified_at"`
	Content      string     `json:"content"`
}

// Handler returns the web UI HTTP handler: the page at "/" and the JSON feed at
// "/api/updates".
func Handler(fetch FetchFunc) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/updates", func(w http.ResponseWriter, r *http.Request) {
		q := Query{
			Limit:      atoiDefault(r.URL.Query().Get("limit"), 50),
			Offset:     atoiDefault(r.URL.Query().Get("offset"), 0),
			Category:   r.URL.Query().Get("category"),
			Importance: r.URL.Query().Get("importance"),
		}
		updates, total, err := fetch(r.Context(), q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := make([]updateDTO, 0, len(updates))
		for _, u := range updates {
			d := updateDTO{
				ID:           u.ID,
				FeedID:       u.FeedID,
				Title:        u.Title,
				SourceURL:    u.SourceURL,
				PublishedAt:  u.PublishedAt,
				CreatedAt:    u.CreatedAt,
				Important:    u.VerdictImportant,
				Category:     u.VerdictCategory,
				Confidence:   u.VerdictConfidence,
				Reason:       u.VerdictReason,
				ClassifiedAt: u.ClassifiedAt,
			}
			if u.RawContent != nil {
				// Feed content is untrusted HTML — sanitize before it reaches the browser.
				d.Content = htmlPolicy.Sanitize(u.RawContent.Content)
			}
			out = append(out, d)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items":  out,
			"total":  total,
			"limit":  q.Limit,
			"offset": q.Offset,
		})
	})

	mux.HandleFunc("/feed.atom", func(w http.ResponseWriter, r *http.Request) {
		// Mirror the Web UI's category/importance filters; a feed is a fixed
		// window (latest feedMaxItems), so limit/offset are not exposed.
		q := Query{
			Category:   r.URL.Query().Get("category"),
			Importance: r.URL.Query().Get("importance"),
			Limit:      feedMaxItems,
			Offset:     0,
		}
		updates, _, err := fetch(r.Context(), q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		self := r.URL.RequestURI()
		if r.Host != "" {
			self = "http://" + r.Host + r.URL.RequestURI()
		}
		body, err := renderAtom(self, updates)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		_, _ = w.Write(body)
	})

	mux.HandleFunc("/go-ruh.png", func(w http.ResponseWriter, _ *http.Request) {
		b, err := static.ReadFile("go-ruh.png")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(b)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, err := static.ReadFile("index.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})

	return mux
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return n
	}
	return def
}

// Serve runs the web UI server on addr until ctx is cancelled.
func Serve(ctx context.Context, addr string, fetch FetchFunc) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           Handler(fetch),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// A fresh context is required for shutdown: once ctx is cancelled, deriving
	// the shutdown deadline from it would abort the graceful drain immediately.
	go func() { // #nosec G118 -- background context is intentional for graceful shutdown
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
