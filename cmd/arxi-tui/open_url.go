package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode"
)

// This file is open_url: the model opens a page in the USER's browser. Before it, the
// model was told to do it with `run` and a different command per system (start, open,
// xdg-open), which went wrong in three ways a tool can close: a URL with "&" broke
// Windows' `start`, WSL has no xdg-open, and a model that did not know the system opened
// nothing and said it had.
//
// What it will open: http and https addresses, nothing else. A file:// or a custom scheme
// can start a program, and "open this page" is not a way to run one. The address is put
// to the user first, like a command, because opening a page sends them somewhere and the
// address can carry whatever the model put in it.

const toolOpenURL = "open_url"

// maxURLLen bounds an address: no real page needs more, and a model that writes a
// kilobyte of query string is not opening a page.
const maxURLLen = 2048

// openURLTimeout is how long the opener may take to hand the page over. A browser that
// starts keeps running; this is only the wait for the small program that asks it.
const openURLTimeout = 10 * time.Second

// checkURL returns the address to open, or why it cannot be.
func checkURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("open_url needs a url")
	}
	if len(raw) > maxURLLen {
		return "", fmt.Errorf("that address is %d bytes long; the most open_url takes is %d", len(raw), maxURLLen)
	}
	// url.Parse refuses control characters (ESC, NUL) itself; the tests pin that, so a
	// Go that stopped doing it would be noticed. It does accept a space, which is not
	// part of an address and would split the argument on some systems' openers.
	if strings.IndexFunc(raw, unicode.IsSpace) >= 0 {
		return "", errors.New("the address has a space in it; write it percent-encoded")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("that is not an address: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("open_url opens http and https addresses only, not %q; to open a file use run", u.Scheme)
	}
	if u.Hostname() == "" {
		return "", errors.New("the address has no host")
	}
	return raw, nil
}

// openerCommand is the program and arguments that hand an address to the user's browser
// on this system. It is pure so each system's answer can be tested from any other.
// wsl is true under Windows Subsystem for Linux, where the browser is Windows' own;
// have says whether a program is installed.
func openerCommand(goos string, wsl bool, have func(string) bool, addr string) (string, []string) {
	switch goos {
	case "windows":
		// rundll32 takes the address as one argument. `cmd /c start` would read an
		// "&" in it as the end of the command.
		return "rundll32", []string{"url.dll,FileProtocolHandler", addr}
	case "darwin":
		return "open", []string{addr}
	}
	if wsl {
		if have("wslview") {
			return "wslview", []string{addr}
		}
		if have("rundll32.exe") {
			return "rundll32.exe", []string{"url.dll,FileProtocolHandler", addr}
		}
	}
	return "xdg-open", []string{addr}
}

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	b, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

// openInBrowser asks the system to open addr in the user's browser. It does not wait
// for the browser: the opener returns once the page is handed over.
func openInBrowser(addr string) error {
	have := func(name string) bool { _, err := exec.LookPath(name); return err == nil }
	name, args := openerCommand(runtime.GOOS, runtime.GOOS == "linux" && isWSL(), have, addr)
	ctx, cancel := context.WithTimeout(context.Background(), openURLTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("%s did not answer within %s", name, openURLTimeout)
	case errors.Is(err, exec.ErrWaitDelay):
		// The opener left the browser running with the pipe open: it worked.
		return nil
	case err != nil:
		msg := strings.TrimSpace(cleanShellText(string(out)))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s failed: %s", name, shortenRunes(msg, 200))
	}
	return nil
}
