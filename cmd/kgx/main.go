// Command kgx is the run shorthand: `kgx <combination> <program> [args...]`,
// equivalent to `kg run` (ADR-0007). It has no management subcommands.
package main

import (
	"os"

	"github.com/mrchi/keygrp/internal/cli"
)

func main() {
	os.Exit(cli.KGX(os.Args[1:]))
}
