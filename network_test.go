// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
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
		t.Skip("TEXMF_NETWORK non défini: ce test contacte le réseau")
	}
	for _, b := range []Bundle{Beamer} {
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
				t.Fatalf("%s: condensat %s, épinglé %s — l'amont a changé sous le pin",
					src.Describe(), got, b.SHA256)
			}
			n, err := extractZip(data, b.Prefix, t.TempDir()+"/out")
			if err != nil {
				t.Fatalf("extraction: %v", err)
			}
			t.Logf("%s: %d fichiers sous %s", src.Describe(), n, b.Prefix)
			if n < 50 {
				t.Errorf("%d fichiers seulement — l'archive n'a pas la forme attendue", n)
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
		t.Skip("TEXMF_NETWORK non défini: ce test contacte le réseau")
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
				t.Fatalf("%s: %v", oci.Describe(), err)
			}
			sum := sha256.Sum256(data)
			if got := hex.EncodeToString(sum[:]); got != b.SHA256 {
				t.Fatalf("%s: condensat %s, épinglé %s — le miroir ne sert pas les octets épinglés",
					oci.Describe(), got, b.SHA256)
			}
			t.Logf("%s: %d octets, condensat conforme", oci.Describe(), len(data))
		})
	}
}
