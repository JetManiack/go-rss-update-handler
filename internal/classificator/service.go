package classificator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jetbrains/go-rss-update-handler/internal/llm"
	"github.com/jetbrains/go-rss-update-handler/internal/model"
	"github.com/jetbrains/go-rss-update-handler/internal/storage"
)

type Service interface {
	Classify(ctx context.Context, update model.UpdateEvent) (storage.Verdict, error)
}

// PromptManager renders a named prompt blueprint into system and user messages.
type PromptManager interface {
	Execute(ctx context.Context, name string, data any) (system, user string, err error)
}

type service struct {
	llm     llm.Client
	prompts PromptManager
	repo    storage.UpdateRepo
}

func New(llm llm.Client, prompts PromptManager, repo storage.UpdateRepo) Service {
	return &service{llm: llm, prompts: prompts, repo: repo}
}

func (s *service) Classify(ctx context.Context, update model.UpdateEvent) (storage.Verdict, error) {
	// 1. Fetch context (2 latest important updates for the feed)
	// We need feedID for LastImportant. model.UpdateEvent should contain FeedID.
	history, err := s.repo.LastImportant(ctx, update.FeedID, 2)
	if err != nil {
		return storage.Verdict{}, fmt.Errorf("fetch history: %w", err)
	}

	// 2. Render prompt (system + user) from the classify blueprint
	data := map[string]any{
		"Current": update,
		"History": history,
	}
	system, user, err := s.prompts.Execute(ctx, "classify", data)
	if err != nil {
		return storage.Verdict{}, fmt.Errorf("render prompt: %w", err)
	}

	// 3. Call LLM
	llmReq := llm.Request{
		System:      system,
		User:        user,
		JSONMode:    true,
		Temperature: 0.1,
	}

	llmResp, err := s.llm.Complete(ctx, llmReq)
	if err != nil {
		return storage.Verdict{}, fmt.Errorf("llm call: %w", err)
	}

	// 4. Parse response
	var v struct {
		Important  bool    `json:"important"`
		Category   string  `json:"category"`
		Confidence float64 `json:"confidence"`
		Reason     string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(llmResp.Content), &v); err != nil {
		return storage.Verdict{}, fmt.Errorf("unmarshal llm response: %w", err)
	}

	return storage.Verdict{
		Important:  v.Important,
		Category:   v.Category,
		Confidence: v.Confidence,
		Reason:     v.Reason,
	}, nil
}
