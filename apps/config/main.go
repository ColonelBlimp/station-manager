package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(os.Stderr, "PANIC in main: %v\n", r)
			_, _ = fmt.Fprintf(os.Stderr, "Stack trace:\n%s\n", debug.Stack())
			os.Exit(ExitPanic)
		}
	}()

	workingDir, err := utils.WorkingDir()
	if err != nil {
		errors.PrintChain(err)
		_, _ = fmt.Fprintf(os.Stderr, "failed to determine working directory: %v\n", errors.Root(err))
		os.Exit(ExitWorkingDir)
	}

	fmt.Println(workingDir)
}
