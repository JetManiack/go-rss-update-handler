package main

import (
	"context"
	"github.com/urfave/cli/v3"
	"os"
	"testing"
)

func TestRun(t *testing.T) {
	os.Args = []string{"gruh", "--config", "test.yaml", "--check-config"}

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config"},
			&cli.BoolFlag{Name: "check-config"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.String("config") != "test.yaml" {
				t.Errorf("expected config test.yaml, got %s", cmd.String("config"))
			}
			if !cmd.Bool("check-config") {
				t.Errorf("expected check-config true")
			}
			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
}
