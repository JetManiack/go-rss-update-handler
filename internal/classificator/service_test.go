package classificator

import (
	"context"
	"testing"

	"github.com/jetbrains/go-rss-update-handler/internal/llm"
	"github.com/jetbrains/go-rss-update-handler/internal/model"
	"github.com/jetbrains/go-rss-update-handler/internal/storage"
)

type mockLLM struct{}
func (m *mockLLM) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	return llm.Response{Content: `{"important": true, "category": "security", "confidence": 0.9, "reason": "important"}`}, nil
}

type mockPrompts struct{}
func (m *mockPrompts) Render(name string, data any) (string, error) {
	return "prompt content", nil
}

type stubRepo struct{}
func (r *stubRepo) InsertNew(ctx context.Context, updates []storage.Update) ([]storage.Update, error) { return nil, nil }
func (r *stubRepo) SaveVerdict(ctx context.Context, updateID string, v storage.Verdict) error { return nil }
func (r *stubRepo) LastImportant(ctx context.Context, feedID string, n int) ([]storage.Update, error) { return nil, nil }
func (r *stubRepo) MarkDispatched(ctx context.Context, updateID string, channel string) error { return nil }

func TestClassify(t *testing.T) {
	svc := New(&mockLLM{}, &mockPrompts{}, &stubRepo{})
	verdict, err := svc.Classify(context.Background(), model.UpdateEvent{FeedID: "1"})
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if !verdict.Important {
		t.Errorf("expected important=true")
	}
	if verdict.Category != "security" {
		t.Errorf("expected category security, got %s", verdict.Category)
	}
}
