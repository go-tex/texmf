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
				t.Errorf("clé %q pour un bundle nommé %q", name, b.Name)
			}
			if b.Version == "" {
				t.Error("aucune version")
			}
			if len(b.SHA256) != 64 {
				t.Errorf("digest de %d caractères, attendu 64", len(b.SHA256))
			}
			if strings.ToLower(b.SHA256) != b.SHA256 {
				t.Error("le digest doit être en minuscules")
			}
			if len(b.Prefixes) == 0 {
				t.Error("aucun préfixe : l'archive entière serait aplatie")
			}
			for _, p := range b.Prefixes {
				if !strings.HasSuffix(p, "/") {
					t.Errorf("le préfixe %q devrait finir par /", p)
				}
			}
			if len(b.Sources) < 2 {
				t.Error("il faut au moins deux routes : le miroir et l'amont")
			}
			if _, ok := UpstreamURL(b); !ok {
				t.Error("aucune route amont : le bundle devient injoignable si le miroir tombe")
			}
			if !strings.Contains(mustUpstream(t, b), b.Version) {
				t.Error("l'URL amont ne nomme pas la version épinglée")
			}
		})
	}
}

func mustUpstream(t *testing.T, b Bundle) string {
	t.Helper()
	u, ok := UpstreamURL(b)
	if !ok {
		t.Fatal("pas d'amont")
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
		t.Fatalf("obtenu %v, attendu [pgf pgfplots] dans cet ordre", names)
	}
}

// A bundle with no dependencies is returned alone, and nothing is repeated.
func TestWithDependenciesIsStable(t *testing.T) {
	if got := WithDependencies(Beamer); len(got) != 1 || got[0].Name != "beamer" {
		t.Errorf("beamer seul attendu, obtenu %d entrées", len(got))
	}
	if got := WithDependencies(PGF); len(got) != 1 || got[0].Name != "pgf" {
		t.Errorf("pgf seul attendu, obtenu %d entrées", len(got))
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
			t.Errorf("%s : préfixes %v, il faut les deux moitiés", b.Name, b.Prefixes)
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
		t.Errorf("une dépendance absente du catalogue devrait être ignorée, obtenu %d entrées", len(got))
	}
	All["zzcycle"] = Bundle{Name: "zzcycle"}
	defer delete(All, "zzcycle")
	if got := WithDependencies(All["zzcycle"]); len(got) != 1 {
		t.Errorf("un cycle devrait se terminer sur une seule entrée, obtenu %d", len(got))
	}
}

// UpstreamURL reports no route when a bundle has only mirrors — the case the
// publisher must not mistake for "nothing to mirror".
func TestUpstreamURLWithoutAnHTTPSource(t *testing.T) {
	b := Bundle{Name: "zz", Sources: []Source{OCISource{Registry: "ghcr.io", Repository: "zz/zz", Reference: "1"}}}
	if _, ok := UpstreamURL(b); ok {
		t.Error("un bundle sans route HTTP ne devrait pas en annoncer une")
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
					t.Errorf("Lookup(%q) = %s, attendu aucun", c.ask, b.Name)
				}
				return
			}
			if !ok || b.Name != c.want {
				t.Errorf("Lookup(%q) = (%s, %v), attendu %s", c.ask, b.Name, ok, c.want)
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
			t.Errorf("%s ne se retrouve pas par son propre nom", b.Name)
		}
		for _, p := range b.Provides {
			if other, clash := owner[p]; clash {
				t.Errorf("%q est revendiqué par %s et par %s", p, other, b.Name)
			}
			owner[p] = b.Name
		}
	}
}
