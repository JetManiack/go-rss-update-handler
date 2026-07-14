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

	"github.com/jetbrains/go-rss-update-handler/internal/storage"
)

//go:embed index.html
var static embed.FS

// htmlPolicy sanitizes untrusted feed HTML before it is rendered in the UI:
// it keeps common formatting and strips scripts, event handlers, etc.
var htmlPolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.RequireNoFollowOnLinks(true)
	return p
}()

// FetchFunc returns the most recent updates (with raw content) for display.
type FetchFunc func(ctx context.Context, limit int) ([]storage.Update, error)

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
		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		updates, err := fetch(r.Context(), limit)
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
		_ = json.NewEncoder(w).Encode(out)
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
