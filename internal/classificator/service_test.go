package classificator

import (
	"context"
	"errors"
	"testing"

	"github.com/jetbrains/go-rss-update-handler/internal/llm"
	"github.com/jetbrains/go-rss-update-handler/internal/model"
)

type mockLLM struct {
	content string
	err     error
}

func (m *mockLLM) Complete(_ context.Context, _ llm.Request) (llm.Response, error) {
	if m.err != nil {
		return llm.Response{}, m.err
	}
	return llm.Response{Content: m.content}, nil
}

type mockPrompts struct{}

func (m *mockPrompts) Execute(_ context.Context, _ string, _ any) (string, string, error) {
	return "system", "user", nil
}

func newSvc(content string) Service {
	return New(&mockLLM{content: content}, &mockPrompts{}, Config{ConfidenceThreshold: 0.5, MaxFormatRetries: 2})
}

func TestClassify_ImportantAboveThreshold(t *testing.T) {
	svc := newSvc(`{"title": "Foo v1.0 — big new features", "important": true, "category": "release", "confidence": 0.9, "reason": "big"}`)
	v, err := svc.Classify(context.Background(), model.UpdateEvent{FeedID: "1"}, nil)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !v.Important {
		t.Error("expected important=true above the confidence threshold")
	}
	if v.Category != "release" {
		t.Errorf("category = %q, want release", v.Category)
	}
	if v.Title != "Foo v1.0 — big new features" {
		t.Errorf("title = %q", v.Title)
	}
}

func TestClassify_LowConfidenceIsNoise(t *testing.T) {
	svc := newSvc(`{"important": true, "category": "release", "confidence": 0.3, "reason": "meh"}`)
	v, err := svc.Classify(context.Background(), model.UpdateEvent{FeedID: "1"}, nil)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if v.Important {
		t.Error("a below-threshold confidence must be treated as noise")
	}
}

func TestClassify_SecurityAlwaysImportant(t *testing.T) {
	// Low confidence and important=false, but security is forced important.
	svc := newSvc(`{"important": false, "category": "security", "confidence": 0.1, "reason": "CVE"}`)
	v, err := svc.Classify(context.Background(), model.UpdateEvent{FeedID: "1"}, nil)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !v.Important {
		t.Error("security updates must always be important")
	}
}

func TestClassify_FormatRetryThenFail(t *testing.T) {
	// Always-invalid JSON: after retries, a non-important "failed" verdict, no error.
	svc := newSvc(`not json`)
	v, err := svc.Classify(context.Background(), model.UpdateEvent{FeedID: "1"}, nil)
	if err != nil {
		t.Fatalf("Classify must not error on a format failure: %v", err)
	}
	if v.Important {
		t.Error("a failed classification must not be important")
	}
}

func TestClassify_LLMErrorFailsFast(t *testing.T) {
	svc := New(&mockLLM{err: errors.New("boom")}, &mockPrompts{}, Config{ConfidenceThreshold: 0.5})
	if _, err := svc.Classify(context.Background(), model.UpdateEvent{FeedID: "1"}, nil); err == nil {
		t.Fatal("expected a fail-fast error when the LLM call fails")
	}
}
