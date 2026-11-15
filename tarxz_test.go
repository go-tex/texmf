// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/go-compressions/xz"
)

// The two fixtures under testdata are OUR OWN bytes, not a redistributed
// package: a tar of four files this test wrote, compressed twice with different
// LZMA2 dictionary settings. This module redistributes nothing, and that applies
// to its fixtures as much as to its bundles — so the archive shape is exercised
// without a byte of anyone's TeX in the repository.
//
// They are committed rather than built at test time on purpose. Building them
// would need the xz CLI, and a test that skips where a tool is absent asserts
// nothing on the runner that lacks it.
const (
	xzFixture    = "testdata/demo.tar.xz"         // --lzma2=dict=8MiB, as TeX Live's own
	xzBigDictFix = "testdata/demo-bigdict.tar.xz" // --lzma2=dict=256MiB, above the ceiling
)

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// An xz'd tar reads exactly as a gzipped one does: entries under a prefix,
// flattened by base name, .lua dropped, everything else ignored.
func TestAnXZTarReadsLikeAGzippedOne(t *testing.T) {
	files, err := readArchive(readFixture(t, xzFixture), []string{"tex/latex/demo/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("%d file(s), want 2: %v", len(files), keysOf(files))
	}
	// The content, so this asserts a DECOMPRESSION and not merely a tar walk.
	if got := string(files["demo.sty"]); !strings.Contains(got, `\ProvidesPackage{demo}`) {
		t.Errorf("demo.sty did not decompress to its own content: %q", got)
	}
	if _, ok := files["demo-two.sty"]; !ok {
		t.Errorf("demo-two.sty is missing: %v", keysOf(files))
	}
	if _, ok := files["demo.lua"]; ok {
		t.Error("demo.lua was kept; .lua is dropped on the way out")
	}
	if _, ok := files["readme.txt"]; ok {
		t.Error("readme.txt is outside the prefix and was kept anyway")
	}
}

// ⛔⛔ MEMORY FOLLOWS THE DATA, NOT THE HEADER — and the first draft of this
// test asserted the opposite, so it is written out here.
//
// An .xz stream DECLARES the dictionary window its blocks may use. A decoder
// that believes the declaration allocates it before any output exists, where
// neither the LimitReader in readTarXZ nor the catalogue's digest can see it
// coming: the digest of a bomb is still a digest. go-compressions/xz instead
// allocates lazily and bounds what the stream actually NEEDS.
//
// So this fixture — the same tar, compressed with `--lzma2=dict=256MiB`, which
// `xz -lvv` reports as "Memory needed: 257 MiB", four times this module's
// ceiling — DECODES, and allocates almost nothing, because 23 KiB of data needs
// a 23 KiB window. I expected a refusal and asserted one; the accept is the
// property, and asserting the refusal would have pinned a decoder that rejects
// legitimate archives for what their header says.
func TestADeclaredDictionaryIsNotAnAllocation(t *testing.T) {
	files, err := readArchive(readFixture(t, xzBigDictFix), []string{"tex/latex/demo/"})
	if err != nil {
		t.Fatalf("a 256 MiB DECLARED window refused a 23 KiB payload: %v — the ceiling "+
			"must bound what a stream needs, not what it announces", err)
	}
	if len(files) != 2 {
		t.Errorf("%d file(s), want 2: %v", len(files), keysOf(files))
	}
}

// ⛔ AND THE CEILING IS WIRED, which the test above cannot show precisely
// because it passes either way. Lowering it below what the fixture needs must
// refuse, with the decoder's own error and before the allocation.
//
// ⚠ The ceiling must also ADMIT THE POPULATION, the half no refusal test shows:
// measured 2026-11-15, the archive the natbib bundle pins declares
// `--lzma2=dict=8MiB` ("Memory needed: 9 MiB") against a ceiling of 64 MiB, and
// the passing fixture above carries that same 8 MiB — so the accepted case is
// shaped like the real one rather than like a minimum.
//
// No GOMEMLIMIT or ulimit is needed here, and that is the whole point: the
// refusal happens before the allocation, so there is no allocation to bound.
func TestTheDictionaryCeilingIsWired(t *testing.T) {
	defer func(old int) { maxDictionary = old }(maxDictionary)
	maxDictionary = 4 << 10 // smaller than any real LZMA2 window

	_, err := readArchive(readFixture(t, xzFixture), []string{"tex/latex/demo/"})
	if err == nil {
		t.Fatal("a 4 KiB ceiling accepted a stream needing more: readTarXZ is not " +
			"passing maxDictionary to the decoder")
	}
	if !errors.Is(err, xz.ErrTooLarge) {
		t.Errorf("err = %v, want xz.ErrTooLarge: the refusal must come from the "+
			"ceiling and not from some later failure that happens to error", err)
	}
}

// The sniff, both ways: xz is dispatched by its own magic and nothing else is.
func TestXZIsRecognisedByItsMagicAlone(t *testing.T) {
	if !isXZ(readFixture(t, xzFixture)) {
		t.Error("the xz fixture was not recognised")
	}
	for name, b := range map[string][]byte{
		"gzip":  {0x1f, 0x8b, 0x08, 0},
		"zip":   []byte("PK\x03\x04"),
		"empty": {},
		"short": {0xfd, '7', 'z'},
		"near":  {0xfd, '7', 'z', 'X', 'Z', 0x01}, // last byte wrong
	} {
		if isXZ(b) {
			t.Errorf("%s was taken for xz", name)
		}
	}
}

// A stream with the right magic and nothing behind it is an error, not a panic:
// the bytes come off someone else's server.
//
// ⚠ THE LAST BYTE IS NOT IN THIS LIST, and that is measured rather than
// overlooked. Dropping one byte is ACCEPTED: a tar ends at its two zero blocks,
// which sit before the .xz stream footer, so the entries are all read and
// returned before the truncation is reached. That is how both formats work
// rather than a hole — a cut archive that still contains every file yields
// every file — and asserting a refusal there would pin a behaviour neither
// format has.
func TestATruncatedXZStreamIsAnError(t *testing.T) {
	full := readFixture(t, xzFixture)
	for _, n := range []int{len(xzMagic), 12, 20, len(full) / 2, len(full) - 200} {
		if _, err := readArchive(full[:n], []string{"tex/latex/demo/"}); err == nil {
			t.Errorf("%d byte(s) of the stream were accepted", n)
		}
	}
}

// The catalogue reaches natbib by the bundle's name and by each package name a
// document would write, which is the whole point of Provides: no document says
// \usepackage{natbib} to get bibentry.
func TestNatbibIsReachableByEveryNameItProvides(t *testing.T) {
	for _, name := range []string{"natbib", "bibentry"} {
		b, ok := Lookup(name)
		if !ok {
			t.Errorf("Lookup(%q) found nothing", name)
			continue
		}
		if b.Name != Natbib.Name {
			t.Errorf("Lookup(%q) = %q, want %q", name, b.Name, Natbib.Name)
		}
	}
	if _, ok := All[Natbib.Name]; !ok {
		t.Error("natbib is not in All, so the publish workflow cannot mirror it")
	}
	if Natbib.SHA256 == "" || len(Natbib.Sources) == 0 || len(Natbib.Prefixes) == 0 {
		t.Errorf("the natbib entry is incomplete: %+v", Natbib)
	}
}
