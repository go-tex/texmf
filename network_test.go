// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestUpstreamPinsStillHold is the one test that uses the network, and it does
// not run unless TEXMF_NETWORK is set — the rest of the suite must stay
// deterministic and offline.
//
// What it guards: a catalogue entry pins a release by digest, and a pin is a
// claim about someone else's server. A tag re-cut upstream, a release asset
// renamed, a URL that quietly 404s — all of them turn a working module into one
// that fails at a user's first cold download. CI runs this so the failure lands
// here instead.
func TestUpstreamPinsStillHold(t *testing.T) {
	if os.Getenv("TEXMF_NETWORK") == "" {
		t.Skip("TEXMF_NETWORK unset: this test reaches the network")
	}
	// ⛔ DRIVEN FROM THE CATALOGUE, NOT FROM A HAND-WRITTEN LIST. This read
	// `[]Bundle{Beamer}` while All held six bundles, so five upstream pins were
	// never checked — including the two font routes, which are the most fragile
	// of the set: a sourceforge download path and a GitHub release asset. A test
	// whose subject list is typed by hand drifts from the population it covers,
	// and nothing says so when a bundle is added.
	names := make([]string, 0, len(All))
	for name := range All {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b := All[name]
		t.Run(b.Name+"@"+b.Version, func(t *testing.T) {
			// Only the LAST source is checked: it is the upstream release, the
			// one this project does not control and therefore the one whose
			// drift matters. The registry route is verified by publishing it.
			src := b.Sources[len(b.Sources)-1]
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			data, err := src.Fetch(ctx)
			if err != nil {
				t.Fatalf("%s: %v", src.Describe(), err)
			}
			sum := sha256.Sum256(data)
			if got := hex.EncodeToString(sum[:]); got != b.SHA256 {
				t.Fatalf("%s: digest %s, pinned %s — upstream changed under the pin",
					src.Describe(), got, b.SHA256)
			}
			n, err := extractArchive(data, b.Prefixes, t.TempDir()+"/out")
			if err != nil {
				t.Fatalf("extraction: %v", err)
			}
			t.Logf("%s: %d fichiers sous %v", src.Describe(), n, b.Prefixes)
			// ⛔ A FLOOR MUST HOLD FOR EVERY BUNDLE OR IT IS A FLOOR FOR ONE.
			// This was `n < 50`, a beamer-shaped number: beamer extracts 129
			// files, and natbib extracts TWO — natbib.sty and bibentry.sty are
			// the whole of what a TeX engine wants from it. Tied instead to a
			// fact the bundle declares: it cannot answer more package names than
			// it ships files, so one file per Provides entry is the least it can
			// be and still be the archive it claims.
			if n < len(b.Provides) {
				t.Errorf("%d file(s) extracted for %d name(s) in Provides %v — the "+
					"archive is not the shape this entry claims", n, len(b.Provides), b.Provides)
			}
		})
	}
}

// TestMirrorMatchesThePin is what the publish workflow runs after pushing: it
// pulls the artifact back through this module's OWN client — the same code a
// user runs — and checks it against the pin.
//
// A push that publishes something the client cannot read is not a published
// bundle, and a mirror that serves other bytes than the pin is worse than no
// mirror at all. Neither is visible from the push side.
func TestMirrorMatchesThePin(t *testing.T) {
	if os.Getenv("TEXMF_NETWORK") == "" {
		t.Skip("TEXMF_NETWORK unset: this test reaches the network")
	}
	for _, b := range []Bundle{Beamer} {
		t.Run(b.Name+"@"+b.Version, func(t *testing.T) {
			var oci Source
			for _, s := range b.Sources {
				if _, ok := s.(OCISource); ok {
					oci = s
					break
				}
			}
			if oci == nil {
				t.Skipf("%s n'a pas de route de registre", b.Name)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			data, err := oci.Fetch(ctx)
			if err != nil {
				// A newly created ghcr package is PRIVATE, and an anonymous pull
				// of one answers 401 — which is what this looks like the first
				// time a bundle is mirrored. Say so, because "401 Unauthorized"
				// on its own reads like a credentials problem in the job that
				// just pushed successfully.
				hint := ""
				if strings.Contains(err.Error(), "401") {
					hint = "\n\nAn anonymous 401 on a freshly published package means " +
						"almost always that it is still PRIVATE. GitHub does not allow " +
						"a container package's visibility cannot be changed through the API: do it " +
						"in the package settings, then re-run this job."
				}
				t.Fatalf("%s: %v%s", oci.Describe(), err, hint)
			}
			sum := sha256.Sum256(data)
			if got := hex.EncodeToString(sum[:]); got != b.SHA256 {
				t.Fatalf("%s: digest %s, pinned %s — the mirror is not serving the pinned bytes",
					oci.Describe(), got, b.SHA256)
			}
			t.Logf("%s: %d octets, condensat conforme", oci.Describe(), len(data))
		})
	}
}
