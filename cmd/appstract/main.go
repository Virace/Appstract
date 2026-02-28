package main

import (
	"os"

	"appstract/internal/cli"
)

func main() {
	code := cli.Execute(os.Args[1:], os.Stdout, os.Stderr, os.Getenv("APPSTRACT_HOME"))
	os.Exit(code)
}
