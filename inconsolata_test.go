// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"strings"
	"testing"
)

// The Inconsolata entry is pinned like every other: a digest, a registry route
// named for the revision, an upstream route naming the same revision, and only
// the font directory extracted.
func TestInconsolataEntryIsComplete(t *testing.T) {
	b, ok := All["inconsolata"]
	if !ok || b.Name != Inconsolata.Name {
		t.Fatal("inconsolata is not in All")
	}
	if len(b.SHA256) != 64 {
		t.Errorf("digest %q is not a SHA-256", b.SHA256)
	}
	if b.Version != "r79618" {
		t.Errorf("version %q, want the TeX Live revision r79618", b.Version)
	}
	if len(b.Prefixes) != 1 || b.Prefixes[0] != "fonts/opentype/public/inconsolata/" {
		t.Errorf("prefixes %v, want only the OpenType directory", b.Prefixes)
	}
	if len(b.Sources) != 2 {
		t.Fatalf("%d sources, want registry then upstream", len(b.Sources))
	}
	oci, ok := b.Sources[0].(OCISource)
	if !ok || oci.Reference != b.Version || !strings.HasSuffix(oci.Repository, "/inconsolata") {
		t.Errorf("first source %+v is not the registry route for %s", b.Sources[0], b.Version)
	}
	h, ok := b.Sources[1].(HTTPSource)
	if !ok || !strings.HasSuffix(h.URL, "inconsolata."+b.Version+".tar.xz") {
		t.Errorf("last source %+v is not the upstream archive for %s", b.Sources[1], b.Version)
	}
}
