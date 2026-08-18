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
	Name:    "beamer",
	Version: beamerVersion,
	SHA256:  "2ab4acf4c6be0d96d3f18161b08fd8e56ab54d380686f6440a42705e48c76f76",
	Prefix:  "tex/latex/beamer/",
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

// All is the catalogue, keyed by bundle name. The publish workflow reads a pin
// from here rather than repeating it in YAML, so a mirror can never carry a
// version or digest this code does not name.
var All = map[string]Bundle{
	Beamer.Name: Beamer,
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
