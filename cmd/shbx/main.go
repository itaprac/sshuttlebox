package main

import (
	"fmt"
	"os"

	"github.com/itaprac/sshuttlebox/internal/cli"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--internal-askpass" {
		if err := cli.RunAskpass(os.Args[2:]); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
