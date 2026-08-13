// Command kg is the keygrp CLI: management verbs plus the `run` verb that
// injects a profile combination into a target program (ADR-0007).
package main

import (
	"os"

	"github.com/mrchi/keygrp/internal/cli"
)

func main() {
	os.Exit(cli.KG(os.Args[1:]))
}
