// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildZip makes a TDS-shaped archive: wanted files under the prefix, plus the
// documentation an extraction must leave behind.
func buildZip(t *testing.T, prefix string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(prefix + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, junk := range []string{"doc/latex/beamer/beameruserguide.pdf", "source/latex/beamer/Makefile"} {
		w, err := zw.Create(junk)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(bytes.Repeat([]byte("x"), 512)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// serveZip stands up a real HTTP server returning the archive, and reports how
// many times it was asked.
func serveZip(t *testing.T, data []byte) (url string, hits *int) {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &n
}

func testBundle(t *testing.T, data []byte, sources ...Source) Bundle {
	t.Helper()
	return Bundle{Name: "zztest", Version: "1.0", SHA256: digest(data), Prefixes: []string{"tex/latex/zz/"}, Sources: sources}
}

func TestOpenFetchesExtractsAndResolves(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{
		"zz.cls":      "\\ProvidesClass{zz}\n",
		"zzbase.sty":  "% base\n",
		"zztheme.sty": "% theme\n",
	})
	url, hits := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "essai"})

	dir := t.TempDir()
	tree, err := Open(context.Background(), b, Options{CacheDir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := tree.Names(); strings.Join(got, ",") != "zz.cls,zzbase.sty,zztheme.sty" {
		t.Errorf("Names() = %v", got)
	}
	body, ok := tree.Resolve("zzbase.sty")
	if !ok || string(body) != "% base\n" {
		t.Errorf("Resolve(zzbase.sty) = %q, %v", body, ok)
	}
	// TeX asks by base name, but a path must resolve too.
	if _, ok := tree.Resolve("some/where/zz.cls"); !ok {
		t.Error("un chemin devrait se résoudre par son nom de base")
	}
	if _, ok := tree.Resolve("absent.sty"); ok {
		t.Error("un nom inconnu ne devrait pas se résoudre")
	}
	// The documentation and sources of a TDS archive must not have been written.
	if _, err := os.Stat(filepath.Join(tree.Dir(), "beameruserguide.pdf")); !os.IsNotExist(err) {
		t.Error("un fichier hors préfixe a été extrait")
	}
	if *hits != 1 {
		t.Errorf("l'archive a été téléchargée %d fois", *hits)
	}

	// A second Open uses the cache: no further request.
	if _, err := Open(context.Background(), b, Options{CacheDir: dir}); err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if *hits != 1 {
		t.Errorf("le cache n'a pas servi: %d requêtes", *hits)
	}
}

// The digest is the trust anchor: a source that returns other bytes is refused,
// however well it answers.
func TestWrongDigestIsRefused(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
	other := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "falsifié\n"})
	url, _ := serveZip(t, other)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "menteur"})

	dir := t.TempDir()
	_, err := Open(context.Background(), b, Options{CacheDir: dir})
	if err == nil {
		t.Fatal("une archive au mauvais condensat a été acceptée")
	}
	if !strings.Contains(err.Error(), "digest is") {
		t.Errorf("erreur = %v, attendu un refus de condensat", err)
	}
}

// The first source is a route, not a single point of failure: when it fails the
// next one is tried, and the bundle still arrives.
func TestFallsBackToTheNextSource(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "en panne", http.StatusServiceUnavailable)
	}))
	defer down.Close()
	url, hits := serveZip(t, data)
	b := testBundle(t, data,
		HTTPSource{URL: down.URL, Label: "registre"},
		HTTPSource{URL: url, Label: "amont"})

	var log []string
	dir := t.TempDir()
	tree, err := Open(context.Background(), b, Options{CacheDir: dir, Log: func(s string) { log = append(log, s) }})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, ok := tree.Resolve("zz.cls"); !ok {
		t.Error("le repli n'a pas fourni le fichier")
	}
	if *hits != 1 {
		t.Errorf("l'amont a été appelé %d fois", *hits)
	}
	joined := strings.Join(log, "\n")
	if !strings.Contains(joined, "registre failed") || !strings.Contains(joined, "trying amont") {
		t.Errorf("le journal ne rend pas compte du repli:\n%s", joined)
	}
}

func TestEverySourceFailing(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "non", http.StatusNotFound)
	}))
	defer down.Close()
	b := testBundle(t, data, HTTPSource{URL: down.URL, Label: "a"}, HTTPSource{URL: down.URL, Label: "b"})
	_, err := Open(context.Background(), b, Options{CacheDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "no source yielded") {
		t.Fatalf("erreur = %v", err)
	}
	// Both failures must be reported, not only the last.
	if !strings.Contains(err.Error(), "a:") || !strings.Contains(err.Error(), "b:") {
		t.Errorf("les deux échecs devraient être rapportés: %v", err)
	}
}

func TestNoSources(t *testing.T) {
	b := Bundle{Name: "zzvide", Version: "1.0", Prefixes: []string{"tex/"}}
	_, err := Open(context.Background(), b, Options{CacheDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "no sources") {
		t.Fatalf("erreur = %v", err)
	}
}

// Offline never reaches the network: absent from the cache it fails, and present
// it succeeds without a single request.
func TestOfflineUsesOnlyTheCache(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
	url, hits := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "amont"})
	dir := t.TempDir()

	_, err := Open(context.Background(), b, Options{CacheDir: dir, Offline: true})
	if !errors.Is(err, ErrNotCached) {
		t.Fatalf("hors ligne sans cache: erreur = %v, attendu ErrNotCached", err)
	}
	if *hits != 0 {
		t.Fatalf("le mode hors ligne a émis %d requête(s)", *hits)
	}
	if _, err := Open(context.Background(), b, Options{CacheDir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), b, Options{CacheDir: dir, Offline: true}); err != nil {
		t.Fatalf("hors ligne avec cache: %v", err)
	}
	if *hits != 1 {
		t.Errorf("%d requêtes au total, attendu 1", *hits)
	}
}

// An extraction that finds nothing under the prefix is an error, not an empty
// bundle that would later look like a missing file.
func TestPrefixMatchingNothing(t *testing.T) {
	data := buildZip(t, "tex/latex/autre/", map[string]string{"zz.cls": "bon\n"})
	url, _ := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "amont"})
	_, err := Open(context.Background(), b, Options{CacheDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "no entries under") {
		t.Fatalf("erreur = %v", err)
	}
}

// An interrupted extraction must not be mistaken for a finished one: without the
// marker, the directory is refilled.
func TestPartialExtractionIsNotTakenForComplete(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
	url, hits := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "amont"})
	dir := t.TempDir()
	if _, err := Open(context.Background(), b, Options{CacheDir: dir}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "zztest@1.0", ".complete")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), b, Options{CacheDir: dir}); err != nil {
		t.Fatal(err)
	}
	if *hits != 2 {
		t.Errorf("%d requêtes, attendu 2 — l'extraction partielle aurait dû être refaite", *hits)
	}
}

func TestCorruptArchive(t *testing.T) {
	data := []byte("ceci n'est pas un zip")
	url, _ := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "amont"})
	_, err := Open(context.Background(), b, Options{CacheDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "reading zip") {
		t.Fatalf("erreur = %v", err)
	}
}

// A zip bomb is not caught by a digest — the digest of a bomb is a valid digest.
func TestOversizedEntryIsRefused(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("tex/latex/zz/enorme.sty")
	w.Write(bytes.Repeat([]byte("A"), maxFileSize+1))
	zw.Close()
	data := buf.Bytes()
	url, _ := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "amont"})
	_, err := Open(context.Background(), b, Options{CacheDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("erreur = %v", err)
	}
}

func TestHTTPSourceDescribe(t *testing.T) {
	if got := (HTTPSource{URL: "https://x/y.zip"}).Describe(); got != "https://x/y.zip" {
		t.Errorf("Describe() = %q", got)
	}
	if got := (HTTPSource{URL: "https://x/y.zip", Label: "amont"}).Describe(); got != "amont" {
		t.Errorf("Describe() = %q", got)
	}
}

func TestHTTPSourceBadRequest(t *testing.T) {
	if _, err := (HTTPSource{URL: "://pas-une-url"}).Fetch(context.Background()); err == nil {
		t.Error("une URL invalide devrait échouer")
	}
	if _, err := (HTTPSource{URL: "http://127.0.0.1:1"}).Fetch(context.Background()); err == nil {
		t.Error("un hôte injoignable devrait échouer")
	}
}

func TestHTTPSourceRefusesAnOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("A"), maxArchiveSize+1))
	}))
	defer srv.Close()
	_, err := HTTPSource{URL: srv.URL}.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("erreur = %v", err)
	}
}

// ── OCI ─────────────────────────────────────────────────────────────────────

// ociServer stands up a registry serving one single-layer artifact.
func ociServer(t *testing.T, repo, ref string, blob []byte, opts ...func(*ociOpts)) *httptest.Server {
	t.Helper()
	o := ociOpts{token: "jeton", tokenField: "token"}
	for _, f := range opts {
		f(&o)
	}
	dig := "sha256:" + digest(blob)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/token"):
			if o.noToken {
				http.Error(w, "pas de jeton anonyme", http.StatusUnauthorized)
				return
			}
			if o.badTokenBody {
				w.Write([]byte("pas du json"))
				return
			}
			json.NewEncoder(w).Encode(map[string]string{o.tokenField: o.token})
		case r.URL.Path == "/v2/"+repo+"/manifests/"+ref:
			// Auth is demanded only when a usable token was actually issued: a
			// registry that offers none, or answers the token endpoint with
			// rubbish, must still serve an unauthenticated pull.
			issued := !o.noToken && !o.badTokenBody
			if got := r.Header.Get("Authorization"); issued && got != "Bearer "+o.token {
				http.Error(w, "jeton absent: "+got, http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", mediaManifest)
			json.NewEncoder(w).Encode(map[string]any{
				"layers": o.layers(dig, len(blob)),
			})
		case r.URL.Path == "/v2/"+repo+"/blobs/"+dig:
			w.Write(blob)
		default:
			http.Error(w, "inconnu: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type ociOpts struct {
	token        string
	tokenField   string
	noToken      bool
	badTokenBody bool
	layerCount   int
}

func (o ociOpts) layers(dig string, size int) []map[string]any {
	n := o.layerCount
	if n == 0 {
		n = 1
	}
	out := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, map[string]any{"digest": dig, "size": size})
	}
	return out
}

func newOCISource(srv *httptest.Server, repo, ref string) OCISource {
	return OCISource{Registry: strings.TrimPrefix(srv.URL, "http://"), Repository: repo, Reference: ref}
}

// The OCI route is exercised against a real registry conversation — token,
// manifest, blob — not a stub of our own code.
func TestOCISourcePullsTheLayer(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
	for _, field := range []string{"token", "access_token"} {
		srv := ociServer(t, "go-tex/texmf/zz", "1.0", data, func(o *ociOpts) { o.tokenField = field })
		src := newOCISource(srv, "go-tex/texmf/zz", "1.0")
		// The test server speaks http; point the source at it directly.
		got, err := src.fetchVia(context.Background(), srv.Client(), "http://"+src.Registry)
		if err != nil {
			t.Fatalf("%s: %v", field, err)
		}
		if !bytes.Equal(got, data) {
			t.Errorf("%s: couche différente de l'archive", field)
		}
	}
}

func TestOCISourceDescribe(t *testing.T) {
	s := OCISource{Registry: "ghcr.io", Repository: "go-tex/texmf/beamer", Reference: "3.77"}
	if got := s.Describe(); got != "OCI artifact ghcr.io/go-tex/texmf/beamer:3.77" {
		t.Errorf("Describe() = %q", got)
	}
	s.Label = "registre"
	if got := s.Describe(); got != "registre" {
		t.Errorf("Describe() = %q", got)
	}
}

// A registry with no anonymous token, and one whose token body is not JSON: both
// fall through to an unauthenticated pull rather than failing outright.
func TestOCISourceWithoutAnonymousToken(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
	for _, tweak := range []func(*ociOpts){
		func(o *ociOpts) { o.noToken = true },
		func(o *ociOpts) { o.badTokenBody = true },
	} {
		srv := ociServer(t, "zz", "1.0", data, tweak)
		src := newOCISource(srv, "zz", "1.0")
		got, err := src.fetchVia(context.Background(), srv.Client(), "http://"+src.Registry)
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !bytes.Equal(got, data) {
			t.Error("couche différente de l'archive")
		}
	}
}

func TestOCISourceRefusesAMultiLayerManifest(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
	srv := ociServer(t, "zz", "1.0", data, func(o *ociOpts) { o.layerCount = 2 })
	src := newOCISource(srv, "zz", "1.0")
	_, err := src.fetchVia(context.Background(), srv.Client(), "http://"+src.Registry)
	if err == nil || !strings.Contains(err.Error(), "exactly one layer") {
		t.Fatalf("erreur = %v", err)
	}
}

func TestOCISourceMissingArtifact(t *testing.T) {
	data := buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
	srv := ociServer(t, "zz", "1.0", data)
	src := newOCISource(srv, "zz", "absent")
	_, err := src.fetchVia(context.Background(), srv.Client(), "http://"+src.Registry)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("erreur = %v", err)
	}
}

func TestOCISourceUnreachableRegistry(t *testing.T) {
	src := OCISource{Registry: "127.0.0.1:1", Repository: "zz", Reference: "1.0"}
	if _, err := src.Fetch(context.Background()); err == nil {
		t.Error("un registre injoignable devrait échouer")
	}
}

func TestOCISourceBadManifest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			json.NewEncoder(w).Encode(map[string]string{"token": "t"})
			return
		}
		w.Write([]byte("pas du json"))
	}))
	defer srv.Close()
	src := OCISource{Registry: strings.TrimPrefix(srv.URL, "http://"), Repository: "zz", Reference: "1.0"}
	_, err := src.fetchVia(context.Background(), srv.Client(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "parsing manifest") {
		t.Fatalf("erreur = %v", err)
	}
}

// The catalogue entry must stay self-consistent: the digest is 64 hex chars and
// the version appears in both routes, so raising one without the other shows up
// here rather than at a user's first download.
func TestBeamerCatalogueEntryIsConsistent(t *testing.T) {
	if len(Beamer.SHA256) != 64 {
		t.Errorf("SHA256 fait %d caractères", len(Beamer.SHA256))
	}
	if _, err := hex.DecodeString(Beamer.SHA256); err != nil {
		t.Errorf("SHA256 n'est pas hexadécimal: %v", err)
	}
	if len(Beamer.Sources) != 2 {
		t.Fatalf("%d routes, attendu 2", len(Beamer.Sources))
	}
	for _, s := range Beamer.Sources {
		if !strings.Contains(s.Describe(), Beamer.Version) {
			t.Errorf("la route %q ne mentionne pas la version %s", s.Describe(), Beamer.Version)
		}
	}
	for _, p := range Beamer.Prefixes {
		if !strings.HasSuffix(p, "/") {
			t.Errorf("le préfixe %q devrait finir par /", p)
		}
	}
	if len(Beamer.Prefixes) == 0 {
		t.Error("beamer ne nomme aucun préfixe")
	}
}

func TestBundleDirUsesTheUserCacheByDefault(t *testing.T) {
	got, err := bundleDir(Beamer, Options{})
	if err != nil {
		t.Fatal(err)
	}
	base, _ := os.UserCacheDir()
	want := filepath.Join(base, "go-tex", "texmf", "beamer@"+Beamer.Version)
	if got != want {
		t.Errorf("bundleDir = %q, attendu %q", got, want)
	}
	// filepath.Join, not a string built with slashes: this test runs on Windows
	// too, where the separator is not "/".
	custom := filepath.Join(t.TempDir(), "zz")
	if got, err := bundleDir(Beamer, Options{CacheDir: custom}); err != nil ||
		got != filepath.Join(custom, "beamer@"+Beamer.Version) {
		t.Errorf("bundleDir(CacheDir) = %q, %v", got, err)
	}
}

func TestSortStrings(t *testing.T) {
	s := []string{"c", "a", "b", "a"}
	sortStrings(s)
	if fmt.Sprint(s) != "[a a b c]" {
		t.Errorf("sortStrings → %v", s)
	}
}
