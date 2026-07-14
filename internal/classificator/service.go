package classificator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jetbrains/go-rss-update-handler/internal/llm"
	"github.com/jetbrains/go-rss-update-handler/internal/model"
	"github.com/jetbrains/go-rss-update-handler/internal/storage"
)

// Service classifies an update into an importance verdict. History (the recent
// important updates for the feed) is supplied by the caller — the classificator
// does not read from storage itself.
type Service interface {
	Classify(ctx context.Context, update model.UpdateEvent, history []storage.Update) (storage.Verdict, error)
}

// PromptManager renders a named prompt blueprint into system and user messages.
type PromptManager interface {
	Execute(ctx context.Context, name string, data any) (system, user string, err error)
}

type service struct {
	llm     llm.Client
	prompts PromptManager
	cfg     Config
}

func New(llmClient llm.Client, prompts PromptManager, cfg Config) Service {
	return &service{llm: llmClient, prompts: prompts, cfg: cfg}
}

// verdictJSON is the raw shape the LLM is asked to produce.
type verdictJSON struct {
	Title      string  `json:"title"`
	Important  bool    `json:"important"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

func (s *service) Classify(ctx context.Context, update model.UpdateEvent, history []storage.Update) (storage.Verdict, error) {
	data := map[string]any{
		"Current": update,
		"History": history,
	}
	system, user, err := s.prompts.Execute(ctx, "classify", data)
	if err != nil {
		return storage.Verdict{}, fmt.Errorf("render prompt: %w", err)
	}

	var lastParseErr error
	for attempt := 0; attempt <= s.cfg.MaxFormatRetries; attempt++ {
		userMsg := user
		if lastParseErr != nil {
			userMsg = user + "\n\nYour previous reply was not valid: " + lastParseErr.Error() +
				"\nReply with a single valid JSON object only."
		}

		resp, err := s.llm.Complete(ctx, llm.Request{
			System:      system,
			User:        userMsg,
			JSONMode:    true,
			Temperature: 0.1,
		})
		if err != nil {
			// LLM unavailable — fail fast; do not persist a fabricated verdict.
			return storage.Verdict{}, fmt.Errorf("llm call: %w", err)
		}

		v, perr := parseVerdict(resp.Content)
		if perr != nil {
			lastParseErr = perr
			continue
		}
		return s.applyRules(v), nil
	}

	// Exhausted format retries: mark as failed (not important) without crashing
	// the pipeline for a single malformed response.
	return storage.Verdict{
		Important: false,
		Category:  "unclassified",
		Reason:    fmt.Sprintf("classification failed: %v", lastParseErr),
	}, nil
}

// applyRules converts the raw LLM verdict into the stored verdict: a
// below-threshold "important" becomes noise, and a security update is always
// important regardless of confidence.
func (s *service) applyRules(v verdictJSON) storage.Verdict {
	important := v.Important && v.Confidence >= s.cfg.ConfidenceThreshold
	if strings.EqualFold(v.Category, "security") {
		important = true
	}
	return storage.Verdict{
		Important:  important,
		Category:   v.Category,
		Confidence: v.Confidence,
		Reason:     v.Reason,
		Title:      v.Title,
	}
}

func parseVerdict(content string) (verdictJSON, error) {
	var v verdictJSON
	if err := json.Unmarshal([]byte(extractJSONObject(content)), &v); err != nil {
		return verdictJSON{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if v.Confidence < 0 || v.Confidence > 1 {
		return verdictJSON{}, fmt.Errorf("confidence %v out of range [0,1]", v.Confidence)
	}
	return v, nil
}

// extractJSONObject returns the substring from the first '{' to the last '}'.
// Some models wrap their JSON in a markdown code fence (```json … ```) or add
// stray prose despite JSON mode; this tolerates that.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
