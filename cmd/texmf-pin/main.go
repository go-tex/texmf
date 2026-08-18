// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

// Command texmf-pin prints one catalogue bundle's pin as key=value lines, so
// that the publish workflow mirrors exactly the version and digest the Go code
// names — and cannot drift from it.
package main

import (
	"fmt"
	"os"

	"github.com/go-tex/texmf"
)

// stdout and osExit are the seams that make main testable: a test redirects the
// first and captures the second, so the entry point is covered like any other
// code rather than being the one function nobody runs.
var (
	stdout      = os.Stdout
	osExit      = os.Exit
	commandLine = func() []string { return os.Args[1:] }
)

func main() { osExit(run(commandLine())) }

func run(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: texmf-pin <bundle>")
		return 2
	}
	b, ok := texmf.Lookup(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "texmf-pin: unknown bundle %q\n", args[0])
		return 1
	}
	url, ok := texmf.UpstreamURL(b)
	if !ok {
		fmt.Fprintf(os.Stderr, "texmf-pin: %s has no upstream route\n", b.Name)
		return 1
	}
	fmt.Fprintf(stdout, "version=%s\n", b.Version)
	fmt.Fprintf(stdout, "sha256=%s\n", b.SHA256)
	fmt.Fprintf(stdout, "url=%s\n", url)
	return 0
}
