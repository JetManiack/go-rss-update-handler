// Command gruh is the entry point of the go-rss-update-handler application.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run is the actual entry point, separated from main for testability.
func run() error {
	// TODO: wire up CLI (urfave/cli v3), config loading and application startup.
	// See docs/design/00-overview.md §7.
	return nil
}
