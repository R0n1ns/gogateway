package main

import (
	"os"

	"github.com/R0n1ns/gogateway/internal/cli"
)

func main() { os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr)) }
