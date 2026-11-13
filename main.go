package main

import (
	"fmt"

	_ "time/tzdata" // embed timezone database so LoadLocation works everywhere
)

// version is overwritten at release time with -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	fmt.Println("clindar", version)
}
