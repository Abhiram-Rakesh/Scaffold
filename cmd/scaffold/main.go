package main

import (
	"os"

	"github.com/scaffold-tool/scaffold/internal/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		os.Exit(1)
	}
}
