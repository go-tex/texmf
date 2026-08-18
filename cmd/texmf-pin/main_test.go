// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/go-tex/texmf"
)

// capture runs the command with stdout redirected to a pipe.
func capture(t *testing.T, args []string) (string, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := stdout
	stdout = w
	code := run(args)
	stdout = old
	w.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	r.Close()
	return b.String(), code
}

// The workflow parses these lines: their names and their shape are the contract.
func TestPrintsThePinAsWorkflowOutputs(t *testing.T) {
	out, code := capture(t, []string{"beamer"})
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	for _, key := range []string{"version=", "sha256=", "url="} {
		if !strings.Contains(out, key) {
			t.Errorf("sortie sans %q:\n%s", key, out)
		}
	}
	if strings.Count(out, "\n") != 3 {
		t.Errorf("attendu 3 lignes, obtenu:\n%s", out)
	}
	if !strings.Contains(out, "sha256=2ab4acf4") {
		t.Errorf("le condensat épinglé n'apparaît pas:\n%s", out)
	}
}

func TestRefusesBadUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"a", "b"}} {
		if _, code := capture(t, args); code != 2 {
			t.Errorf("run(%v) = %d, attendu 2", args, code)
		}
	}
	if _, code := capture(t, []string{"inconnu"}); code != 1 {
		t.Errorf("un paquet inconnu devrait rendre 1")
	}
}

// main itself, through its seams: the entry point is covered like any other
// code rather than being the one function nobody runs.
func TestMainWiring(t *testing.T) {
	oldExit, oldArgs, oldOut := osExit, commandLine, stdout
	defer func() { osExit, commandLine, stdout = oldExit, oldArgs, oldOut }()

	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	stdout = devnull

	got := -1
	osExit = func(code int) { got = code }
	commandLine = func() []string { return []string{"beamer"} }
	main()
	if got != 0 {
		t.Errorf("main() a rendu %d", got)
	}
	commandLine = func() []string { return []string{"inconnu"} }
	main()
	if got != 1 {
		t.Errorf("main() sur un paquet inconnu a rendu %d", got)
	}
}

// A bundle with no upstream route cannot be mirrored, and the command says so
// instead of printing an empty url= the workflow would then curl.
func TestBundleWithoutAnUpstreamRoute(t *testing.T) {
	texmf.All["zzsansamont"] = texmf.Bundle{Name: "zzsansamont", Version: "1.0"}
	defer delete(texmf.All, "zzsansamont")
	if _, code := capture(t, []string{"zzsansamont"}); code != 1 {
		t.Errorf("code = %d, attendu 1", code)
	}
}
