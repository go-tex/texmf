// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

// This file is the catalogue: the bundles this module knows how to get, each
// pinned to one upstream release and one digest.
//
// A bundle is described here and fetched onto the user's machine; none of it is
// stored in this repository or in any binary built from it. That is what keeps
// the licensing question from arising: beamer offers itself "under the LaTeX
// Project Public License and/or under the GNU Public License", and a download is
// not a redistribution, so nothing here has to choose between them on the user's
// behalf.

// beamerVersion is the pinned upstream release. Raising it means raising the
// digest in the same commit — the two are one fact.
const beamerVersion = "3.77"

// Beamer is the beamer presentation class and the files it loads.
//
// Measured over 10025 real talks: with these files present the engine renders
// 99.9% of them and ~8.7 pages per talk; without them, falling back to the
// engine's own emulation, 92.3% and ~1.4 pages. The difference is not an edge
// case, it is most of the content.
//
// The archive is beamer's TDS distribution. Only tex/latex/beamer/ is extracted:
// the rest is documentation and sources, thousands of files the engine never
// opens.
//
// Routes, in order:
//
//  1. ghcr.io/go-tex/texmf/beamer — a registry the project controls, so the
//     default route does not depend on a third party staying up;
//  2. the upstream release itself, so the bundle stays reachable when the
//     registry is not.
//
// Both must produce the same bytes: SHA256 is checked whichever route ran, so a
// route is a delivery mechanism and never a trust anchor.
var Beamer = Bundle{
	Name:     "beamer",
	Version:  beamerVersion,
	SHA256:   "2ab4acf4c6be0d96d3f18161b08fd8e56ab54d380686f6440a42705e48c76f76",
	Prefixes: []string{"tex/latex/beamer/"},
	Sources: []Source{
		OCISource{
			Registry:   "ghcr.io",
			Repository: "go-tex/texmf/beamer",
			Reference:  beamerVersion,
			Label:      "ghcr.io/go-tex/texmf/beamer:" + beamerVersion,
		},
		HTTPSource{
			URL:   "https://github.com/josephwright/beamer/releases/download/v" + beamerVersion + "/beamer.tds.zip",
			Label: "upstream release josephwright/beamer v" + beamerVersion,
		},
	},
}

// pgfVersion and pgfplotsVersion are the pinned upstream releases. Raising one
// means raising its digest in the same commit — the two are one fact.
const (
	pgfVersion      = "3.1.12"
	pgfplotsVersion = "1.18.2"
)

// PGF is pgf and TikZ: the drawing package the engine emulates when it is absent
// and loads for real when it is present.
//
// Two prefixes, because pgf splits itself the way a package that serves both
// plain TeX and LaTeX has to: the .sty wrappers live under tex/latex/pgf/ and
// almost everything else — tikz.code.tex, pgfcore, pgfkeys, the pgfsys drivers —
// under tex/generic/pgf/. Loading only one half loads nothing.
//
// The .lua files under those trees are dropped on the way out (see readZip): the
// engine has no Lua interpreter, and they are the only base-name collisions pgf
// has.
var PGF = Bundle{
	Name:     "pgf",
	Version:  pgfVersion,
	SHA256:   "d243b67705ab4f0e4fe91c4b26ed9da67cd7b0643fc38f158e91b600df9ba15e",
	Prefixes: []string{"tex/generic/pgf/", "tex/latex/pgf/"},
	Sources: []Source{
		OCISource{
			Registry:   "ghcr.io",
			Repository: "go-tex/texmf/pgf",
			Reference:  pgfVersion,
			Label:      "ghcr.io/go-tex/texmf/pgf:" + pgfVersion,
		},
		HTTPSource{
			URL:   "https://github.com/pgf-tikz/pgf/releases/download/" + pgfVersion + "/pgf_" + pgfVersion + ".tds.zip",
			Label: "upstream release pgf-tikz/pgf " + pgfVersion,
		},
	},
}

// PGFPlots is pgfplots, which draws function plots on top of pgf and cannot load
// without it.
//
// It is the single largest thing standing between the engine and a real paper's
// figures: of the arXiv documents that contain a tikzpicture and draw nothing,
// more than half use pgfplots, and \addplot is by a wide margin the most common
// command the engine reports as undefined.
var PGFPlots = Bundle{
	Name:     "pgfplots",
	Version:  pgfplotsVersion,
	SHA256:   "4c4f33e976ba01d3f635c92d0a697a70c2c8779d5bcaae3b3ec2fbd8c82cc7ce",
	Prefixes: []string{"tex/generic/pgfplots/", "tex/latex/pgfplots/"},
	Sources: []Source{
		OCISource{
			Registry:   "ghcr.io",
			Repository: "go-tex/texmf/pgfplots",
			Reference:  pgfplotsVersion,
			Label:      "ghcr.io/go-tex/texmf/pgfplots:" + pgfplotsVersion,
		},
		HTTPSource{
			URL:   "https://github.com/pgf-tikz/pgfplots/releases/download/" + pgfplotsVersion + "/pgfplots_" + pgfplotsVersion + ".tds.zip",
			Label: "upstream release pgf-tikz/pgfplots " + pgfplotsVersion,
		},
	},
}

// Requires names the bundles a bundle cannot load without, innermost first. It is
// a fact about the packages, not about any one caller, so it belongs beside the
// pins rather than in whatever program happens to fetch them.
var Requires = map[string][]string{
	PGFPlots.Name: {PGF.Name},
}

// WithDependencies returns b preceded by everything it requires, in load order
// and without repeats.
func WithDependencies(b Bundle) []Bundle {
	var out []Bundle
	seen := map[string]bool{}
	var add func(Bundle)
	add = func(x Bundle) {
		if seen[x.Name] {
			return
		}
		seen[x.Name] = true
		for _, dep := range Requires[x.Name] {
			if d, ok := Lookup(dep); ok {
				add(d)
			}
		}
		out = append(out, x)
	}
	add(b)
	return out
}

// All is the catalogue, keyed by bundle name. The publish workflow reads a pin
// from here rather than repeating it in YAML, so a mirror can never carry a
// version or digest this code does not name.
var All = map[string]Bundle{
	Beamer.Name:   Beamer,
	PGF.Name:      PGF,
	PGFPlots.Name: PGFPlots,
}

// Lookup returns a catalogue bundle by name.
func Lookup(name string) (Bundle, bool) {
	b, ok := All[name]
	return b, ok
}

// UpstreamURL returns the bundle's upstream route, which is by convention its
// last one: the registry mirrors exist to be tried first, and the publisher
// needs the source they mirror.
func UpstreamURL(b Bundle) (string, bool) {
	for i := len(b.Sources) - 1; i >= 0; i-- {
		if h, ok := b.Sources[i].(HTTPSource); ok {
			return h.URL, true
		}
	}
	return "", false
}
