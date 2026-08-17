package main

import (
	"fmt"
	"os"

	"github.com/pydevsg/sudiviz-go/internal/mcp"
)

func main() {
	if err := mcp.ServeStdio(); err != nil {
		fmt.Fprintf(os.Stderr, "sudiviz-mcp: %v\n", err)
		os.Exit(1)
	}
}
