package main

import (
	"fmt"
	"os"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "GUI startup failed: %v\n", r)
			if runtime.GOOS == "linux" {
				fmt.Fprintln(os.Stderr, "If you're running headless or over SSH, you need a working X11/Wayland display (DISPLAY/WAYLAND_DISPLAY).")
			}
			os.Exit(1)
		}
	}()

	if err := preflightDesktop(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	a := app.New()
	a.Settings().SetTheme(newNEUTheme())
	w := a.NewWindow("NEUDFS GUI")
	w.Resize(fyne.NewSize(980, 680))

	s := newState(w)
	s.buildUI()
	w.ShowAndRun()
}
