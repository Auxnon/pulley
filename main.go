package main

import (
	"fmt"
	"os"

	"github.com/Auxnon/pulley/pulley"
)

func main() {
	app, err := pulley.NewDefaultApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
