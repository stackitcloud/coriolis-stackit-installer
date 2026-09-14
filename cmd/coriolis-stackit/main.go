package main

import (
	"fmt"
	"os"

	"github.com/stackitcloud/coriolis-stackit-installer/internal/installer"
)

var version = "dev"

func main() {
	if err := installer.Run(os.Args[1:], version); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
