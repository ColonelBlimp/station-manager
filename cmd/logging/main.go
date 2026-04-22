package main

import (
	"fmt"
	"os"

	"gioui.org/app"
)

func main() {

	go func() {
		window := new(app.Window)
		if err := run(window); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(window *app.Window) (err error) {
	if window == nil {
		return fmt.Errorf("window cannot be nil")
	}
	return nil
}
