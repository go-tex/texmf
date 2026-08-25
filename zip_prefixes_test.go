// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"archive/zip"
	"bytes"
	"testing"
)

// zipOf builds an archive holding the named entries.
func zipOf(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("contenu de " + n)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A package that serves both plain TeX and LaTeX splits itself between
// tex/generic/<name>/ and tex/latex/<name>/ and needs both halves. Naming one
// prefix loads half a package, which fails in ways that look like anything but a
// missing file.
func TestReadZipTakesEveryPrefix(t *testing.T) {
	data := zipOf(t,
		"tex/generic/zz/core.code.tex",
		"tex/generic/zz/libs/extra.code.tex",
		"tex/latex/zz/zz.sty",
		"tex/context/zz/ignore.tex",
		"doc/zz/manual.pdf",
	)
	files, err := readZip(data, []string{"tex/generic/zz/", "tex/latex/zz/"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"core.code.tex", "extra.code.tex", "zz.sty"} {
		if _, ok := files[want]; !ok {
			t.Errorf("%s manque", want)
		}
	}
	for _, unwanted := range []string{"ignore.tex", "manual.pdf"} {
		if _, ok := files[unwanted]; ok {
			t.Errorf("%s a été extrait alors qu'il est hors des préfixes", unwanted)
		}
	}
	if len(files) != 3 {
		t.Errorf("%d fichiers extraits, attendu 3", len(files))
	}
}

// A nested directory under a prefix comes along: the prefix is a subtree, not a
// single directory, and pgfplots keeps most of itself two levels down.
func TestReadZipDescendsIntoSubdirectories(t *testing.T) {
	data := zipOf(t, "tex/generic/zz/a/b/c/deep.code.tex")
	files, err := readZip(data, []string{"tex/generic/zz/"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["deep.code.tex"]; !ok {
		t.Error("un fichier profond n'a pas été extrait")
	}
}

// .lua files are dropped: the engine has no Lua interpreter, and they are the
// only base-name collisions pgf has — a collision in a flattened tree silently
// loses one of the two files.
func TestReadZipDropsLuaFiles(t *testing.T) {
	data := zipOf(t,
		"tex/generic/zz/library.lua",
		"tex/latex/zz/library.lua",
		"tex/generic/zz/real.code.tex",
	)
	files, err := readZip(data, []string{"tex/generic/zz/", "tex/latex/zz/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Errorf("%d fichiers, attendu 1 (les .lua sont écartés)", len(files))
	}
	if _, ok := files["real.code.tex"]; !ok {
		t.Error("le fichier TeX manque")
	}
}

// No prefix at all matches nothing, rather than flattening the whole archive.
func TestReadZipWithNoPrefixMatchesNothing(t *testing.T) {
	data := zipOf(t, "tex/generic/zz/a.tex")
	if _, err := readZip(data, nil); err == nil {
		t.Error("une liste de préfixes vide devrait échouer plutôt que tout extraire")
	}
}
