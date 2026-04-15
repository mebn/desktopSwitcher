//go:build windows

package main

import (
	"os"

	"desktopswitcher/internal/desktopswitcher"
)

func main() {
	os.Exit(desktopswitcher.Main(os.Args[1:]))
}
