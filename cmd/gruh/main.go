// Command gruh is the entry point of the go-rss-update-handler application.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jetbrains/go-rss-update-handler/internal/bus"
	"github.com/jetbrains/go-rss-update-handler/internal/classificator"
	"github.com/jetbrains/go-rss-update-handler/internal/collector"
	"github.com/jetbrains/go-rss-update-handler/internal/config"
	"github.com/jetbrains/go-rss-update-handler/internal/deduplicator"
	"github.com/jetbrains/go-rss-update-handler/internal/dispatcher"
	"github.com/jetbrains/go-rss-update-handler/internal/llm"
	"github.com/jetbrains/go-rss-update-handler/internal/observability"
	"github.com/jetbrains/go-rss-update-handler/internal/orchestrator"
	"github.com/jetbrains/go-rss-update-handler/internal/parser"
	"github.com/jetbrains/go-rss-update-handler/internal/prompt"
	"github.com/jetbrains/go-rss-update-handler/internal/scheduler"
	"github.com/jetbrains/go-rss-update-handler/internal/storage"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Name:    "gruh",
		Usage:   "RSS update handler",
		Version: "0.1.0",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "config",
				Usage: "path to configuration file",
				Value: "config.yaml",
			},
			&cli.BoolFlag{
				Name:  "check-config",
				Usage: "validate configuration without starting the service",
			},
		},
		Commands: []*cli.Command{
			{
				Name:   "collector",
				Usage:  "run collector",
				Action: runCollector,
			},
			{
				Name:   "worker",
				Usage:  "run worker",
				Action: runWorker,
			},
			{
				Name:   "dispatcher",
				Usage:  "run dispatcher",
				Action: runDispatcher,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return run(ctx, cmd)
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCollector(ctx context.Context, cmd *cli.Command) error {
	cfg, logger, db, _, err := initEnv(cmd)
	if err != nil {
		return err
	}
	c := collector.NewCollector(cfg.Collector)
	p := parser.NewParser()
	d := deduplicator.NewDeduplicator()
	b := bus.NewMemoryBus()
	orch := orchestrator.NewOrchestrator(c, p, d, b, db.Feeds(), db.Updates(), nil, nil, logger)

	sched := scheduler.NewScheduler(cfg.Scheduler.Interval, float64(cfg.Scheduler.Jitter)/float64(cfg.Scheduler.Interval), nil)
	go sched.Start(ctx, "collector-lock", func(ctx context.Context) {
		feeds, err := db.Feeds().List(ctx)
		if err != nil {
			logger.Error("failed to list feeds", "err", err)
			return
		}
		for _, f := range feeds {
			if err := orch.ProcessFeed(ctx, f); err != nil {
				logger.Error("failed to process feed", "feed", f.URL, "err", err)
			}
		}
	})

	<-ctx.Done()
	return nil
}

func initDispatcher(cfg config.DispatcherConfig) *dispatcher.Service {
	var notifiers []dispatcher.Notifier
	for name, url := range cfg.Slack {
		notifiers = append(notifiers, dispatcher.NewSlackNotifier(name, url))
	}
	for name, params := range cfg.Telegram {
		notifiers = append(notifiers, dispatcher.NewTelegramNotifier(name, params["token"], params["chat_id"]))
	}
	return dispatcher.NewService(notifiers)
}

func runWorker(ctx context.Context, cmd *cli.Command) error {
	cfg, logger, db, b, err := initEnv(cmd)
	if err != nil {
		return err
	}
	llmClient := llm.New(cfg.LLM)
	prompts, err := prompt.New(cfg.Prompt.Dir)
	if err != nil {
		return err
	}
	classificatorSvc := classificator.New(llmClient, prompts, cfg.Classificator)
	disp := initDispatcher(cfg.Dispatcher)
	orch := orchestrator.NewOrchestrator(nil, nil, nil, b, db.Feeds(), db.Updates(), classificatorSvc, disp, logger)
	return orch.RunWorker(ctx)
}

func runDispatcher(ctx context.Context, cmd *cli.Command) error {
	cfg, logger, db, b, err := initEnv(cmd)
	if err != nil {
		return err
	}
	disp := initDispatcher(cfg.Dispatcher)
	orch := orchestrator.NewOrchestrator(nil, nil, nil, b, db.Feeds(), db.Updates(), nil, disp, logger)
	return orch.RunDispatcher(ctx)
}

func initEnv(cmd *cli.Command) (*config.Config, *slog.Logger, storage.Store, bus.Bus, error) {
	cfg, err := config.Load(cmd.String("config"))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	logger := observability.NewLogger(cfg.Observability.Log)
	db, _, err := storage.InitDB(cfg.Storage)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	b := bus.NewMemoryBus() // Use RedisBus in the future
	return cfg, logger, db, b, nil
}

// run is the actual entry point, separated from main for testability.
func run(ctx context.Context, cmd *cli.Command) error {
	cfgPath := cmd.String("config")
	checkOnly := cmd.Bool("check-config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if checkOnly {
		fmt.Println("configuration OK")
		return nil
	}

	logger := observability.NewLogger(cfg.Observability.Log)
	logger.Info("starting gruh")

	ctx, cancel := observability.NotifyShutdown(ctx)
	defer cancel()

	db, gormDB, err := storage.InitDB(cfg.Storage)
	if err != nil {
		return fmt.Errorf("init db: %w", err)
	}

	// Periodic raw_contents retention cleanup (no-op when retention is 0).
	retention, err := storage.ParseRetention(cfg.Storage.RawContentRetention)
	if err != nil {
		return fmt.Errorf("parse retention: %w", err)
	}
	go storage.StartRetentionJob(ctx, gormDB, retention, time.Hour, logger)

	b := bus.NewMemoryBus()
	c := collector.NewCollector(cfg.Collector)
	p := parser.NewParser()
	d := deduplicator.NewDeduplicator()
	llmClient := llm.New(cfg.LLM)
	prompts, err := prompt.New(cfg.Prompt.Dir)
	if err != nil {
		return fmt.Errorf("new prompt registry: %w", err)
	}
	classificatorSvc := classificator.New(llmClient, prompts, cfg.Classificator)
	disp := initDispatcher(cfg.Dispatcher)
	orch := orchestrator.NewOrchestrator(c, p, d, b, db.Feeds(), db.Updates(), classificatorSvc, disp, logger)

	// Classification worker.
	go func() {
		if err := orch.RunWorker(ctx); err != nil {
			logger.Error("orchestrator worker failed", "err", err)
		}
	}()

	// Notification dispatcher.
	go func() {
		if err := orch.RunDispatcher(ctx); err != nil {
			logger.Error("orchestrator dispatcher failed", "err", err)
		}
	}()

	// Collection scheduler: poll all active feeds on the configured interval.
	jitterFrac := 0.0
	if cfg.Scheduler.Interval > 0 {
		jitterFrac = float64(cfg.Scheduler.Jitter) / float64(cfg.Scheduler.Interval)
	}
	sched := scheduler.NewScheduler(cfg.Scheduler.Interval, jitterFrac, nil)
	go sched.Start(ctx, "collector", func(ctx context.Context) {
		feeds, err := db.Feeds().List(ctx)
		if err != nil {
			logger.Error("failed to list feeds", "err", err)
			return
		}
		for _, f := range feeds {
			if err := orch.ProcessFeed(ctx, f); err != nil {
				logger.Error("failed to process feed", "feed", f.URL, "err", err)
			}
		}
	})

	// Start metrics server
	if cfg.Observability.Metrics != "" {
		go func() {
			logger.Info("starting metrics server", "addr", cfg.Observability.Metrics)
			if err := observability.StartMetricsServer(ctx, cfg.Observability.Metrics); err != nil {
				logger.Error("metrics server failed", "err", err)
			}
		}()
	}

	<-ctx.Done()
	logger.Info("shutting down")
	return nil
}
