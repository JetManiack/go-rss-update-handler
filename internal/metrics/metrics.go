// Package metrics defines the application's Prometheus metrics. They register on
// the default registry, which the observability metrics server exposes at
// /metrics, so callers just increment them — no wiring through constructors.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// FeedFetches counts feed fetch attempts by result: ok | not_modified | error.
	FeedFetches = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gruh_collector_fetches_total",
		Help: "Feed fetch attempts, labelled by result.",
	}, []string{"result"})

	// UpdatesNew counts new (deduplicated) updates persisted.
	UpdatesNew = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gruh_updates_new_total",
		Help: "New, deduplicated updates stored.",
	})

	// Classifications counts classification outcomes: important | noise.
	Classifications = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gruh_classifications_total",
		Help: "Classifications, labelled by outcome.",
	}, []string{"outcome"})

	// LLMRequests counts LLM requests by result: ok | error.
	LLMRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gruh_llm_requests_total",
		Help: "LLM requests, labelled by result.",
	}, []string{"result"})

	// LLMDuration observes LLM request latency in seconds.
	LLMDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "gruh_llm_request_duration_seconds",
		Help:    "LLM request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// LLMTokens counts LLM token usage by kind: prompt | completion.
	LLMTokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gruh_llm_tokens_total",
		Help: "LLM tokens used, labelled by kind.",
	}, []string{"kind"})

	// Dispatches counts notification dispatch results: ok | error.
	Dispatches = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gruh_dispatches_total",
		Help: "Notification dispatches, labelled by result.",
	}, []string{"result"})
)
