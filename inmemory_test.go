// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// OpenInMemory is what a browser needs: Go's js/wasm filesystem shim answers
// ENOSYS to most calls, so Open — which extracts into the user cache — cannot
// run there, even though the whole reason the engine has Options.Resolve is to
// serve a host with no filesystem.

func TestOpenInMemoryResolvesWithoutTouchingTheDisk(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{
		"zz.cls":     "\\ProvidesClass{zz}\n",
		"zzbase.sty": "% base\n",
	})
	url, hits := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "amont"})

	// Every filesystem seam is made to fail: nothing here may touch the disk.
	swap(t, &osMkdirAll, func(string, fs.FileMode) error { return errBoom })
	swap(t, &osMkdirTemp, func(string, string) (string, error) { return "", errBoom })
	swap(t, &osWriteFile, func(string, []byte, fs.FileMode) error { return errBoom })
	swap(t, &osReadDir, func(string) ([]os.DirEntry, error) { return nil, errBoom })
	swap(t, &osUserCacheDir, func() (string, error) { return "", errBoom })

	tree, err := OpenInMemory(context.Background(), b, Options{})
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	if got := tree.Dir(); got != "" {
		t.Errorf("Dir() = %q, attendu vide pour un arbre en mémoire", got)
	}
	if got := strings.Join(tree.Names(), ","); got != "zz.cls,zzbase.sty" {
		t.Errorf("Names() = %q", got)
	}
	body, ok := tree.Resolve("zzbase.sty")
	if !ok || string(body) != "% base\n" {
		t.Errorf("Resolve(zzbase.sty) = %q, %v", body, ok)
	}
	if _, ok := tree.Resolve("chemin/vers/zz.cls"); !ok {
		t.Error("un chemin devrait se résoudre par son nom de base")
	}
	if _, ok := tree.Resolve("absent.sty"); ok {
		t.Error("un nom inconnu ne devrait pas se résoudre")
	}
	if *hits != 1 {
		t.Errorf("%d requêtes, attendu 1", *hits)
	}
}

// The digest is checked here too: an in-memory tree is not a looser one.
func TestOpenInMemoryRefusesAWrongDigest(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
	other := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "falsifié\n"})
	url, _ := serveZip(t, other)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "menteur"})
	if _, err := OpenInMemory(context.Background(), b, Options{}); err == nil ||
		!strings.Contains(err.Error(), "digest is") {
		t.Fatalf("erreur = %v", err)
	}
}

// Offline has no meaning without a cache, and says so rather than reaching the
// network anyway.
func TestOpenInMemoryOffline(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
	url, hits := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "amont"})
	_, err := OpenInMemory(context.Background(), b, Options{Offline: true})
	if !errors.Is(err, ErrNotCached) {
		t.Fatalf("erreur = %v, attendu ErrNotCached", err)
	}
	if *hits != 0 {
		t.Errorf("le mode hors ligne a émis %d requête(s)", *hits)
	}
}

func TestOpenInMemoryPropagatesFailures(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "non", http.StatusNotFound)
	}))
	defer down.Close()
	if _, err := OpenInMemory(context.Background(), testBundle(t, data,
		HTTPSource{URL: down.URL, Label: "a"}), Options{}); err == nil ||
		!strings.Contains(err.Error(), "no source yielded") {
		t.Fatalf("erreur = %v", err)
	}
	// And an archive whose prefix matches nothing.
	bad := buildZip(t, "tex/latex/autre/", map[string]string{"zz.cls": "bon\n"})
	url, _ := serveZip(t, bad)
	if _, err := OpenInMemory(context.Background(), testBundle(t, bad,
		HTTPSource{URL: url, Label: "amont"}), Options{}); err == nil ||
		!strings.Contains(err.Error(), "no entries under") {
		t.Fatalf("erreur = %v", err)
	}
}

// FromArchive is the entry a browser needs: neither ghcr.io nor the GitHub
// release sends an Access-Control-Allow-Origin header, so a page cannot fetch
// them itself — the bytes arrive some other way and this verifies them.
func TestFromArchive(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n", "zzb.sty": "% b\n"})
	b := testBundle(t, data)

	tree, err := FromArchive(data, b)
	if err != nil {
		t.Fatalf("FromArchive: %v", err)
	}
	if got := strings.Join(tree.Names(), ","); got != "zz.cls,zzb.sty" {
		t.Errorf("Names() = %q", got)
	}
	if body, ok := tree.Resolve("zz.cls"); !ok || string(body) != "bon\n" {
		t.Errorf("Resolve(zz.cls) = %q, %v", body, ok)
	}

	// The digest still decides, whoever fetched the bytes.
	other := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "falsifié\n"})
	if _, err := FromArchive(other, b); err == nil || !strings.Contains(err.Error(), "digest is") {
		t.Errorf("erreur = %v, attendu un refus de condensat", err)
	}

	// And an archive with nothing under the prefix is an error, not an empty tree.
	bad := buildZip(t, "tex/latex/autre/", map[string]string{"zz.cls": "bon\n"})
	if _, err := FromArchive(bad, testBundle(t, bad)); err == nil ||
		!strings.Contains(err.Error(), "no entries under") {
		t.Errorf("erreur = %v", err)
	}
}
