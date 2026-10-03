package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// stdinKey reads an API key from standard input for `arxi provider key`.
//
// The key is never a command-line argument: argv lands in shell history and in
// the process table, where any other user of the machine can read it. Standard
// input is neither. Two ways in:
//
//   - piped:   printenv MY_KEY | arxi provider key openai
//   - typed:   a terminal is asked for it with echo switched off, where the
//     platform allows it (see echoOff).
//
// Only the first line is used, and its trailing newline is dropped. The text of
// an error never contains what was read.
func stdinKey(in *os.File, prompt io.Writer) (string, error) {
	if !isTerminal(in) {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("reading the key from standard input: %w", err)
		}
		return nonEmptyKey(line)
	}
	restore, err := echoOff(in)
	if err != nil {
		return "", errors.New("this terminal cannot hide what you type, so the key is not read from it\n" +
			"  pipe it in instead:  printenv MY_KEY | arxi provider key <name>")
	}
	fmt.Fprint(prompt, "API key (input is hidden): ")
	line, rerr := bufio.NewReader(in).ReadString('\n')
	restore()
	fmt.Fprintln(prompt)
	if rerr != nil && rerr != io.EOF {
		return "", fmt.Errorf("reading the key: %w", rerr)
	}
	return nonEmptyKey(line)
}

func nonEmptyKey(line string) (string, error) {
	k := strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(k) == "" {
		return "", errors.New("no key was given on standard input")
	}
	return k, nil
}

// isTerminal reports whether f is a character device.
func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
