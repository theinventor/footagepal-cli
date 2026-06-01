package main

import (
	"fmt"
	"os"

	"github.com/theinventor/footagepal-cli/cmd"
	"github.com/theinventor/footagepal-cli/internal/exitcode"
)

func main() {
	if err := cmd.NewRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "footagepal: %v\n", err)
		os.Exit(exitcode.ExitCodeFor(err))
	}
}
