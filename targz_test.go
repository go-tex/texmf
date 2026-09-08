// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

// tarGz builds a gzipped tar from name→contents, for the tests below.
func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// An upstream release is a zip or a gzipped tar, and which one is a fact about
// the release: beamer and pgf publish TDS zips, Linux Libertine publishes only
// LinLibertineOTF_<version>.tgz. readArchive sniffs it, so the catalogue never
// has to carry a field naming the format.
func TestReadArchiveSniffsTheFormat(t *testing.T) {
	gz := tarGz(t, map[string]string{
		"LinLibertine_R.otf": "regular",
		"LinBiolinum_R.otf":  "biolinum",
		"ChangeLog.txt":      "not a font",
		"docs/manual.lua":    "never opened",
	})
	files, err := readArchive(gz, []string{"LinLibertine_", "LinBiolinum_"})
	if err != nil {
		t.Fatalf("readArchive: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("got %d files, want 2: %v", len(files), keysOf(files))
	}
	if string(files["LinLibertine_R.otf"]) != "regular" {
		t.Errorf("wrong contents: %q", files["LinLibertine_R.otf"])
	}
	if _, ok := files["ChangeLog.txt"]; ok {
		t.Error("a file outside the prefixes was taken")
	}
}

// The entries are flattened to their base names, exactly as the zip path does:
// the engine asks for "LinLibertine_R.otf" and never for a path.
func TestTarGzFlattensAndDropsLua(t *testing.T) {
	gz := tarGz(t, map[string]string{
		"fonts/opentype/public/x/A.otf": "a",
		"fonts/opentype/public/x/b.lua": "dropped",
	})
	files, err := readTarGz(gz, []string{"fonts/opentype/public/x/"})
	if err != nil {
		t.Fatalf("readTarGz: %v", err)
	}
	if _, ok := files["A.otf"]; !ok {
		t.Errorf("not flattened: %v", keysOf(files))
	}
	if _, ok := files["b.lua"]; ok {
		t.Error("a .lua reached the tree: this module serves an engine with no Lua")
	}
}

// A file past maxFileSize is refused rather than written: the digest of a bomb
// is still a digest, so the bound is the only thing that stops one.
func TestTarGzRefusesAnOversizeEntry(t *testing.T) {
	gz := tarGz(t, map[string]string{"big/huge.otf": strings.Repeat("x", maxFileSize+1)})
	if _, err := readTarGz(gz, []string{"big/"}); err == nil {
		t.Fatal("an entry over the bound was accepted")
	}
}

// Nothing under the prefixes is an error, not an empty tree: a bundle that
// silently resolved nothing would look like a working one.
func TestTarGzRefusesAnEmptySelection(t *testing.T) {
	gz := tarGz(t, map[string]string{"other/a.otf": "a"})
	if _, err := readTarGz(gz, []string{"nothing/"}); err == nil {
		t.Fatal("an empty selection was accepted")
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A file whose first two bytes say gzip but whose body is not one is refused by
// the reader rather than by the digest: an archive is checked for what it IS.
func TestTarGzRefusesABodyThatIsNotGzip(t *testing.T) {
	if _, err := readArchive([]byte{0x1f, 0x8b, 'n', 'o'}, []string{"x/"}); err == nil {
		t.Fatal("a truncated gzip was accepted")
	}
}

// A valid gzip holding something that is not a tar fails on the first header,
// not silently as an empty selection — the two are different faults and a caller
// that saw "no entries" would look in the wrong place.
func TestTarGzRefusesAGzipThatIsNotATar(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(strings.Repeat("not a tar header at all", 100))); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readTarGz(buf.Bytes(), []string{"x/"}); err == nil {
		t.Fatal("a gzip that is not a tar was accepted")
	}
}

// An entry whose base name is empty, "." or ".." names no file. Skipping it is
// what keeps a crafted archive from deciding where the extraction writes.
func TestTarGzSkipsEntriesWithNoBaseName(t *testing.T) {
	gz := tarGz(t, map[string]string{
		"fonts/..":    "up",
		"fonts/.":     "here",
		"fonts/A.otf": "a",
	})
	files, err := readTarGz(gz, []string{"fonts/"})
	if err != nil {
		t.Fatalf("readTarGz: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("got %v, want just A.otf", keysOf(files))
	}
}

// A tar whose last header promises more bytes than the archive holds fails on the
// entry rather than yielding a short file. An archive truncated in transit is
// exactly the case a digest cannot catch on its own — the bytes that arrived
// hash to something, just not to the pin — so the reader has to refuse it too.
func TestTarGzRefusesATruncatedEntry(t *testing.T) {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	if err := tw.WriteHeader(&tar.Header{Name: "fonts/A.otf", Mode: 0o644, Size: 4096, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	// The header is in the buffer; the 4096 bytes it promises are not, and the
	// writer is deliberately never closed so no padding or footer follows.
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readTarGz(gz.Bytes(), []string{"fonts/"}); err == nil {
		t.Fatal("a truncated entry was accepted")
	}
}
