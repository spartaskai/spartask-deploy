package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// prompter asks the operator for values; with assumeYes it never reads stdin and uses defaults.
type prompter struct {
	in        *bufio.Reader
	out       io.Writer
	assumeYes bool
}

func newPrompter(assumeYes bool) *prompter {
	return &prompter{in: bufio.NewReader(os.Stdin), out: os.Stdout, assumeYes: assumeYes}
}

func (p *prompter) readLine() (string, error) {
	line, err := p.in.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", fmt.Errorf("read answer: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// ask returns the answer or def; required rejects an empty result.
func (p *prompter) ask(label, def string, required bool) (string, error) {
	if p.assumeYes {
		if required && def == "" {
			return "", fmt.Errorf("%s is required (pass it as a flag with -y)", label)
		}
		return def, nil
	}
	for {
		if def != "" {
			fmt.Fprintf(p.out, "%s [%s]: ", label, def)
		} else {
			fmt.Fprintf(p.out, "%s: ", label)
		}
		answer, err := p.readLine()
		if err != nil {
			return "", err
		}
		if answer == "" {
			answer = def
		}
		if answer != "" || !required {
			return answer, nil
		}
		fmt.Fprintln(p.out, "  A value is required.")
	}
}

// askSecret reads without echo when stdin is a terminal.
func (p *prompter) askSecret(label string) (string, error) {
	if p.assumeYes {
		return "", nil
	}
	fmt.Fprintf(p.out, "%s (input hidden, Enter to skip): ", label)
	if restore := disableEcho(); restore != nil {
		defer func() {
			restore()
			fmt.Fprintln(p.out)
		}()
	}
	return p.readLine()
}

func (p *prompter) confirm(label string, def bool) (bool, error) {
	if p.assumeYes {
		return def, nil
	}
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	for {
		fmt.Fprintf(p.out, "%s [%s]: ", label, hint)
		answer, err := p.readLine()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "":
			return def, nil
		case "y", "yes", "e", "evet":
			return true, nil
		case "n", "no", "h", "hayir", "hayır":
			return false, nil
		}
	}
}

// disableEcho turns off terminal echo through stty (Linux/macOS) and returns the restore func,
// or nil when stdin is not a terminal or stty is unavailable.
func disableEcho() func() {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return nil
	}
	stty, err := exec.LookPath("stty")
	if err != nil {
		return nil
	}
	off := exec.Command(stty, "-echo")
	off.Stdin = os.Stdin
	if off.Run() != nil {
		return nil
	}
	return func() {
		on := exec.Command(stty, "echo")
		on.Stdin = os.Stdin
		_ = on.Run() // best effort: the terminal is restored by the shell at worst
	}
}
