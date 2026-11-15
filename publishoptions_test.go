// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// ⛔⛔ A HAND-WRITTEN LIST OF THE CATALOGUE DRIFTS FROM THE CATALOGUE, and this
// repository had three of them at once: TestUpstreamPinsStillHold checked one
// bundle of six, TestMirrorMatchesThePin still does, and publish.yml's choice
// input offered four of six — linuxlibertine and libertinus were added to All
// and never added there, so NEITHER FONT BUNDLE COULD BE MIRRORED. Nothing
// failed, because nothing compared the two.
//
// GitHub's `type: choice` options cannot be generated, so that list has to stay
// literal. What does not have to stay literal is the AGREEMENT: this reads the
// workflow and requires its options to be exactly the catalogue's names, so the
// next bundle cannot be added without a line there.
//
// ⚠ It parses the one line rather than the YAML, deliberately: a YAML
// dependency to read six words is a dependency this module would carry
// forever, and the line it looks for is the line a reviewer reads too. If the
// workflow is reshaped so the pattern no longer matches, this FAILS rather than
// passing vacuously — the "options:" line not being found is an error here.
func TestPublishWorkflowOffersEveryBundle(t *testing.T) {
	const path = ".github/workflows/publish.yml"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(string(b), "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "options:") {
			line = t
			break
		}
	}
	if line == "" {
		t.Fatalf("%s: no `options:` line found — the workflow was reshaped and this "+
			"check would otherwise pass by looking at nothing", path)
	}
	open, close := strings.Index(line, "["), strings.LastIndex(line, "]")
	if open < 0 || close < open {
		t.Fatalf("%s: `options:` is not an inline list: %q", path, line)
	}
	var got []string
	for _, f := range strings.Split(line[open+1:close], ",") {
		if f = strings.TrimSpace(f); f != "" {
			got = append(got, f)
		}
	}
	sort.Strings(got)

	want := make([]string, 0, len(All))
	for name := range All {
		want = append(want, name)
	}
	sort.Strings(want)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("publish.yml offers %v, the catalogue holds %v — a bundle that is not "+
			"offered cannot be mirrored, and its ghcr route in catalogue.go is then a "+
			"promise nothing keeps", got, want)
	}
}
