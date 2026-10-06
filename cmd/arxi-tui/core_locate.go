package main

import (
	"os"
	"path/filepath"
	"runtime"
)

// coreBinary says which arxi core to launch. ARXI_BIN wins when set (an explicit
// choice is never second-guessed, and checkArxiBin reports a bad one). Otherwise the
// core installed beside this program is used, so a release unpacked into one folder
// (or both programs built into the same one) chats without any setting. An empty
// result means "no core": the caller falls back to the offline demo.
func coreBinary(env string, executable func() (string, error)) string {
	if env != "" {
		return env
	}
	exe, err := executable()
	if err != nil || exe == "" {
		return ""
	}
	name := "arxi"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	candidate := filepath.Join(filepath.Dir(exe), name)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}
	return ""
}
