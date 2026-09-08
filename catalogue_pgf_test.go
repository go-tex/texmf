// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"strings"
	"testing"
)

// Every catalogue entry has to be complete before anything tries to fetch it: a
// missing digest turns a delivery route into a trust anchor, and a missing
// prefix flattens a whole TDS archive.
func TestCatalogueEntriesAreComplete(t *testing.T) {
	for name, b := range All {
		t.Run(name, func(t *testing.T) {
			if b.Name != name {
				t.Errorf("key %q for a bundle named %q", name, b.Name)
			}
			if b.Version == "" {
				t.Error("no version")
			}
			if len(b.SHA256) != 64 {
				t.Errorf("digest of %d characters, want 64", len(b.SHA256))
			}
			if strings.ToLower(b.SHA256) != b.SHA256 {
				t.Error("the digest must be lowercase")
			}
			if len(b.Prefixes) == 0 {
				t.Error("no prefix: the whole archive would be flattened")
			}
			// A prefix is either a directory — and then it must end in "/", or
			// "tex/latex/beam" would also match a beamx/ tree — or a bare name
			// stem, for an archive whose files sit at its root. Linux Libertine
			// publishes LinLibertineOTF_<version>.tgz with the .otf files and the
			// licences side by side at the top level, so "LinLibertine_" is the
			// only way to take the fonts and leave the changelogs.
			for _, p := range b.Prefixes {
				if !strings.HasSuffix(p, "/") && strings.Contains(p, "/") {
					t.Errorf("prefix %q is a path but does not end in /", p)
				}
			}
			if len(b.Sources) < 2 {
				t.Error("at least two routes are needed: the mirror and upstream")
			}
			if _, ok := UpstreamURL(b); !ok {
				t.Error("no upstream route: the bundle becomes unreachable if the mirror falls")
			}
			if !strings.Contains(mustUpstream(t, b), b.Version) {
				t.Error("the upstream URL does not name the pinned version")
			}
		})
	}
}

func mustUpstream(t *testing.T, b Bundle) string {
	t.Helper()
	u, ok := UpstreamURL(b)
	if !ok {
		t.Fatal("no upstream")
	}
	return u
}

// pgfplots cannot load without pgf, and saying so beside the pins is what lets a
// caller fetch the pair without knowing why.
func TestPGFPlotsRequiresPGF(t *testing.T) {
	got := WithDependencies(PGFPlots)
	if len(got) != 2 || got[0].Name != "pgf" || got[1].Name != "pgfplots" {
		var names []string
		for _, b := range got {
			names = append(names, b.Name)
		}
		t.Fatalf("got %v, want [pgf pgfplots] in that order", names)
	}
}

// A bundle with no dependencies is returned alone, and nothing is repeated.
func TestWithDependenciesIsStable(t *testing.T) {
	if got := WithDependencies(Translator); len(got) != 1 || got[0].Name != "translator" {
		t.Errorf("want translator alone, got %d entries", len(got))
	}
	if got := WithDependencies(PGF); len(got) != 1 || got[0].Name != "pgf" {
		t.Errorf("want pgf alone, got %d entries", len(got))
	}
}

// beamer requires translator: beamerbasetranslator.sty loads it for every talk,
// and without it a theme's own words — Theorem, Definition, Example — reach the
// slide as the braces of an undefined \translate.
func TestBeamerRequiresTranslator(t *testing.T) {
	got := WithDependencies(Beamer)
	if len(got) != 2 || got[0].Name != "translator" || got[1].Name != "beamer" {
		var names []string
		for _, b := range got {
			names = append(names, b.Name)
		}
		t.Fatalf("got %v, want [translator beamer] in that order", names)
	}
}

// The two pgf bundles each need both halves of their TDS split: the .sty files
// live under tex/latex/, everything the engine actually reads under tex/generic/.
func TestPGFBundlesNameBothHalves(t *testing.T) {
	for _, b := range []Bundle{PGF, PGFPlots} {
		var generic, latex bool
		for _, p := range b.Prefixes {
			generic = generic || strings.HasPrefix(p, "tex/generic/")
			latex = latex || strings.HasPrefix(p, "tex/latex/")
		}
		if !generic || !latex {
			t.Errorf("%s: prefixes %v, both halves are needed", b.Name, b.Prefixes)
		}
	}
}

// A bundle naming a dependency the catalogue does not hold is skipped rather
// than fetched as a zero Bundle, and a cycle terminates.
func TestWithDependenciesHandlesTheEdges(t *testing.T) {
	Requires["zzorphan"] = []string{"zznexistepas"}
	Requires["zzcycle"] = []string{"zzcycle"}
	defer func() { delete(Requires, "zzorphan"); delete(Requires, "zzcycle") }()

	orphan := Bundle{Name: "zzorphan"}
	if got := WithDependencies(orphan); len(got) != 1 || got[0].Name != "zzorphan" {
		t.Errorf("a dependency absent from the catalogue must be ignored, got %d entries", len(got))
	}
	All["zzcycle"] = Bundle{Name: "zzcycle"}
	defer delete(All, "zzcycle")
	if got := WithDependencies(All["zzcycle"]); len(got) != 1 {
		t.Errorf("a cycle must end on a single entry, got %d", len(got))
	}
}

// UpstreamURL reports no route when a bundle has only mirrors — the case the
// publisher must not mistake for "nothing to mirror".
func TestUpstreamURLWithoutAnHTTPSource(t *testing.T) {
	b := Bundle{Name: "zz", Sources: []Source{OCISource{Registry: "ghcr.io", Repository: "zz/zz", Reference: "1"}}}
	if _, ok := UpstreamURL(b); ok {
		t.Error("a bundle with no HTTP route must not announce one")
	}
}

// A bundle is named for the distribution it comes from; a document names the
// .sty file it wants, and the two are almost never the same word. Matching only
// the bundle name meant \usepackage{tikz} reached nothing, since no archive is
// called tikz — and tikz is how nearly every document asks for pgf.
func TestLookupAnswersPackageNames(t *testing.T) {
	for _, c := range []struct{ ask, want string }{
		{"tikz", "pgf"},
		{"pgf", "pgf"},
		{"pgfkeys", "pgf"},
		{"pgffor", "pgf"},
		{"tikzexternal", "pgf"},
		{"pgfplots", "pgfplots"},
		{"pgfplotstable", "pgfplots"},
		{"beamer", "beamer"},
		{"beamerarticle", "beamer"},
		{"fancyhdr", ""},
		{"", ""},
	} {
		t.Run(c.ask, func(t *testing.T) {
			b, ok := Lookup(c.ask)
			if c.want == "" {
				if ok {
					t.Errorf("Lookup(%q) = %s, want none", c.ask, b.Name)
				}
				return
			}
			if !ok || b.Name != c.want {
				t.Errorf("Lookup(%q) = (%s, %v), want %s", c.ask, b.Name, ok, c.want)
			}
		})
	}
}

// A bundle answers its own name even when it is not in its own Provides list,
// and no two bundles claim the same package name.
func TestProvidesIsUnambiguous(t *testing.T) {
	owner := map[string]string{}
	for _, b := range All {
		if got, ok := Lookup(b.Name); !ok || got.Name != b.Name {
			t.Errorf("%s is not found by its own name", b.Name)
		}
		for _, p := range b.Provides {
			if other, clash := owner[p]; clash {
				t.Errorf("%q is claimed by both %s and %s", p, other, b.Name)
			}
			owner[p] = b.Name
		}
	}
}
