package main

import (
	"os"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/cli"
)

func main() {
	err := cli.NewRootCommand().Execute()
	os.Exit(cli.ExitCode(err))
}
