// Command gruh is the entry point of the go-rss-update-handler application.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Name:    "gruh",
		Usage:   "RSS update handler",
		Version: "0.1.0",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Usage:   "path to configuration file",
				Value:   "config.yaml",
			},
			&cli.BoolFlag{
				Name:  "check-config",
				Usage: "validate configuration without starting the service",
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

// run is the actual entry point, separated from main for testability.
func run(ctx context.Context, cmd *cli.Command) error {
	cfgPath := cmd.String("config")
	checkOnly := cmd.Bool("check-config")

	fmt.Printf("Starting gruh with config: %s, check-only: %v\n", cfgPath, checkOnly)
	return nil
}
