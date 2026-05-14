package main

import (
	"os"

	"github.com/hok/agentawake/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
