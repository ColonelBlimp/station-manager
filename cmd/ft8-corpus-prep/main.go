package main

import (
	"context"
	stderr "errors"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ft8-corpus-prep <file.wav>")
		os.Exit(2)
	}
	wav := os.Args[1]

	ctx := context.Background()
	decodes, err := RunJt9(ctx, wav)
	if stderr.Is(err, ErrJt9NotFound) {
		fmt.Fprintln(os.Stderr, "error: jt9 binary not found on PATH")
		fmt.Fprintln(os.Stderr, "install WSJT-X first, e.g. `sudo dnf install wsjtx` (Fedora) or `sudo apt install wsjtx` (Debian/Ubuntu)")
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	for _, d := range decodes {
		fmt.Println(d.Message)
	}
}
