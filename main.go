package main

import (
	"os"
	_ "time/tzdata" // embed timezone database so LoadLocation works everywhere

	"github.com/jabiOP/clindar/internal/cli"
)

// version is overwritten at release time with -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	cli.Version = version
	os.Exit(cli.Main())
}
