package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

func preflightDesktop() error {
	// Fyne desktop driver needs a display server on Linux.
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" && runtime.GOOS == "linux" {
		return errors.New("no GUI display detected (missing DISPLAY/WAYLAND_DISPLAY). Run this on a desktop session, use SSH X-forwarding, or run locally.")
	}

	// Fyne parses locale env vars; LANG=C can trigger parsing errors.
	if os.Getenv("LANG") == "C" || os.Getenv("LC_ALL") == "C" || os.Getenv("LC_CTYPE") == "C" {
		_ = os.Setenv("LANG", "en_US.UTF-8")
		_ = os.Setenv("LC_ALL", "en_US.UTF-8")
		_ = os.Setenv("LC_CTYPE", "en_US.UTF-8")
		_ = os.Setenv("LC_MESSAGES", "en_US.UTF-8")
		_ = os.Setenv("LANGUAGE", "en_US.UTF-8")
	}

	// Ensure settings storage is writable. In some sandboxed environments $HOME may be read-only.
	cfgDir, err := os.UserConfigDir()
	if err == nil && cfgDir != "" {
		testDir := filepath.Join(cfgDir, "fyne")
		if mkErr := os.MkdirAll(testDir, 0o755); mkErr != nil {
			tmpCfg := filepath.Join(os.TempDir(), "neudfs-gui-config")
			_ = os.Setenv("XDG_CONFIG_HOME", tmpCfg)
			_ = os.MkdirAll(filepath.Join(tmpCfg, "fyne"), 0o755)
		}
	}
	return nil
}
