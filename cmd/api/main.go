package main

import (
	"fmt"
	"os"

	"github.com/danielwangai/dispatchr/internal/transport"
)

func main() {
	if err := transport.RunServer(); err != nil {
		fmt.Fprintf(os.Stderr, "server stopped with error: %v\n", err)
		os.Exit(1)
	}
}
