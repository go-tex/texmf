// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every filesystem failure this package can meet — a full disk, a read-only
// cache, a home directory the process cannot read — is exercised here through
// the seams in seams.go. An error path that has never run is an error path that
// has never been shown to work.

var errBoom = errors.New("panne simulée")

// swap replaces a seam for the length of one test.
func swap[T any](t *testing.T, p *T, v T) {
	t.Helper()
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

func zzZip(t *testing.T) []byte {
	t.Helper()
	return buildZip(t, "tex/latex/zz/", map[string]string{"zz.cls": "bon\n"})
}

// openWith runs Open against a local server, with whatever seam the caller broke.
func openWith(t *testing.T, data []byte) error {
	t.Helper()
	url, _ := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "amont"})
	_, err := Open(context.Background(), b, Options{CacheDir: t.TempDir()})
	return err
}

func TestExtractionSurvivesNothingWhenTheDiskRefuses(t *testing.T) {
	data := zzZip(t)

	t.Run("mkdir du cache", func(t *testing.T) {
		swap(t, &osMkdirAll, func(string, fs.FileMode) error { return errBoom })
		if err := openWith(t, data); !errors.Is(err, errBoom) {
			t.Errorf("erreur = %v", err)
		}
	})
	t.Run("répertoire temporaire", func(t *testing.T) {
		swap(t, &osMkdirTemp, func(string, string) (string, error) { return "", errBoom })
		if err := openWith(t, data); !errors.Is(err, errBoom) {
			t.Errorf("erreur = %v", err)
		}
	})
	t.Run("écriture d'un fichier", func(t *testing.T) {
		swap(t, &osWriteFile, func(string, []byte, fs.FileMode) error { return errBoom })
		if err := openWith(t, data); !errors.Is(err, errBoom) {
			t.Errorf("erreur = %v", err)
		}
	})
	t.Run("écriture du marqueur", func(t *testing.T) {
		real := osWriteFile
		swap(t, &osWriteFile, func(name string, b []byte, m fs.FileMode) error {
			if filepath.Base(name) == ".complete" {
				return errBoom
			}
			return real(name, b, m)
		})
		if err := openWith(t, data); !errors.Is(err, errBoom) {
			t.Errorf("erreur = %v", err)
		}
	})
	t.Run("suppression de l'ancien répertoire", func(t *testing.T) {
		real := osRemoveAll
		swap(t, &osRemoveAll, func(p string) error {
			if strings.Contains(filepath.Base(p), ".tmp-") {
				return real(p)
			}
			return errBoom
		})
		if err := openWith(t, data); !errors.Is(err, errBoom) {
			t.Errorf("erreur = %v", err)
		}
	})
	t.Run("renommage final", func(t *testing.T) {
		swap(t, &osRename, func(string, string) error { return errBoom })
		if err := openWith(t, data); !errors.Is(err, errBoom) {
			t.Errorf("erreur = %v", err)
		}
	})
	t.Run("lecture du répertoire extrait", func(t *testing.T) {
		swap(t, &osReadDir, func(string) ([]os.DirEntry, error) { return nil, errBoom })
		if err := openWith(t, data); !errors.Is(err, errBoom) {
			t.Errorf("erreur = %v", err)
		}
	})
}

// A cache directory whose marker cannot even be stat'ed is an error, not an
// empty bundle that would be silently refetched for ever.
func TestUnreadableCacheMarker(t *testing.T) {
	swap(t, &osStat, func(string) (os.FileInfo, error) { return nil, errBoom })
	if err := openWith(t, zzZip(t)); !errors.Is(err, errBoom) {
		t.Errorf("erreur = %v", err)
	}
}

func TestNoUserCacheDirectory(t *testing.T) {
	swap(t, &osUserCacheDir, func() (string, error) { return "", errBoom })
	_, err := bundleDir(Beamer, Options{})
	if !errors.Is(err, errBoom) {
		t.Fatalf("erreur = %v", err)
	}
	if !strings.Contains(err.Error(), "no user cache directory") {
		t.Errorf("message peu clair: %v", err)
	}
	// Open reports it too, rather than falling back to some other directory.
	if _, err := Open(context.Background(), Beamer, Options{}); !errors.Is(err, errBoom) {
		t.Errorf("Open: erreur = %v", err)
	}
}

// A file indexed at Open time and gone by the time it is read: Resolve says no,
// it does not return half a file.
func TestResolveWhenTheFileVanishes(t *testing.T) {
	data := zzZip(t)
	url, _ := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "amont"})
	dir := t.TempDir()
	tree, err := Open(context.Background(), b, Options{CacheDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	swap(t, &osReadFile, func(string) ([]byte, error) { return nil, errBoom })
	if _, ok := tree.Resolve("zz.cls"); ok {
		t.Error("Resolve devrait échouer quand le fichier a disparu")
	}
	// And an already-cached file is still served: the read happened once.
	*(&osReadFile) = os.ReadFile
	if _, ok := tree.Resolve("zz.cls"); !ok {
		t.Error("Resolve devrait relire le fichier revenu")
	}
	if _, ok := tree.Resolve("zz.cls"); !ok {
		t.Error("le second appel devrait servir depuis le cache mémoire")
	}
}

// A zip entry whose data is corrupt fails the extraction rather than landing a
// truncated macro file the engine would then misparse.
func TestCorruptEntryData(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("tex/latex/zz/zz.cls")
	w.Write([]byte("bon\n"))
	zw.Close()
	raw := buf.Bytes()
	// Corrupt the entry's DATA, not its headers: a local file header is 30 bytes
	// plus the name, so the compressed bytes start right after. The central
	// directory still parses and the entry is still found — it just no longer
	// reads back, which is the failure a truncated download would produce.
	const name = "tex/latex/zz/zz.cls"
	for i := 30 + len(name); i < 30+len(name)+8 && i < len(raw); i++ {
		raw[i] ^= 0xff
	}
	url, _ := serveZip(t, raw)
	b := testBundle(t, raw, HTTPSource{URL: url, Label: "amont"})
	_, err := Open(context.Background(), b, Options{CacheDir: t.TempDir()})
	if err == nil {
		t.Fatal("une entrée corrompue a été acceptée")
	}
}

// Entries whose base name is empty or a directory marker are skipped rather than
// written somewhere unexpected.
func TestDegenerateEntryNames(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"tex/latex/zz/", "tex/latex/zz/./", "tex/latex/zz/zz.cls"} {
		w, _ := zw.Create(name)
		w.Write([]byte("x\n"))
	}
	zw.Close()
	data := buf.Bytes()
	url, _ := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "amont"})
	tree, err := Open(context.Background(), b, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Names(); len(got) != 1 || got[0] != "zz.cls" {
		t.Errorf("Names() = %v, attendu [zz.cls]", got)
	}
}

func TestOpenTreeOnAMissingDirectory(t *testing.T) {
	if _, err := openTree(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("openTree devrait échouer sur un répertoire absent")
	}
}

// ── réseau ──────────────────────────────────────────────────────────────────

// A URL the standard library refuses to turn into a request, on both routes.
func TestRequestConstructionFailures(t *testing.T) {
	bad := "http://\x7f/"
	if _, err := (HTTPSource{URL: bad}).Fetch(context.Background()); err == nil {
		t.Error("HTTPSource: une URL illégale devrait échouer")
	}
	s := OCISource{Registry: "\x7f", Repository: "zz", Reference: "1.0"}
	if _, err := s.token(context.Background(), http.DefaultClient, "http://\x7f"); err == nil {
		t.Error("token: une URL illégale devrait échouer")
	}
	if _, err := s.get(context.Background(), http.DefaultClient, bad, "", "*/*"); err == nil {
		t.Error("get: une URL illégale devrait échouer")
	}
}

// A response that dies mid-body is an error, not a truncated archive that would
// then fail the digest with a confusing message.
func TestTruncatedResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.Write([]byte("court"))
	}))
	defer srv.Close()
	if _, err := (HTTPSource{URL: srv.URL}).Fetch(context.Background()); err == nil {
		t.Error("HTTPSource: un corps tronqué devrait échouer")
	}
	s := OCISource{Registry: strings.TrimPrefix(srv.URL, "http://"), Repository: "zz", Reference: "1.0"}
	if _, err := s.get(context.Background(), srv.Client(), srv.URL, "", "*/*"); err == nil {
		t.Error("get: un corps tronqué devrait échouer")
	}
}

// A blob larger than the bound is refused: a digest cannot protect a disk from
// an archive that never stops arriving.
func TestOCIBlobTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("A"), maxArchiveSize+1))
	}))
	defer srv.Close()
	s := OCISource{Registry: strings.TrimPrefix(srv.URL, "http://"), Repository: "zz", Reference: "1.0"}
	_, err := s.get(context.Background(), srv.Client(), srv.URL, "", "*/*")
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("erreur = %v", err)
	}
}

// The token endpoint being unreachable is not fatal: the pull is simply tried
// without one.
func TestTokenEndpointUnreachable(t *testing.T) {
	s := OCISource{Registry: "127.0.0.1:1", Repository: "zz", Reference: "1.0"}
	if _, err := s.token(context.Background(), http.DefaultClient, "http://127.0.0.1:1"); err == nil {
		t.Error("un point de terminaison injoignable devrait remonter l'erreur")
	}
}

// An entry compressed with a method this build cannot open — a real archive can
// carry one — fails the extraction instead of landing an empty file.
func TestUnsupportedCompressionMethod(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("tex/latex/zz/zz.cls")
	w.Write([]byte("bon\n"))
	zw.Close()
	data := buf.Bytes()
	// zip.Writer refuses to WRITE an unknown method, so the method field is
	// patched afterwards, in the central directory the reader actually consults:
	// "PK\x01\x02" then the method at offset 10.
	i := bytes.Index(data, []byte("PK\x01\x02"))
	if i < 0 {
		t.Fatal("annuaire central introuvable")
	}
	data[i+10], data[i+11] = 99, 0
	url, _ := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "amont"})
	if _, err := Open(context.Background(), b, Options{CacheDir: t.TempDir()}); err == nil {
		t.Fatal("une méthode de compression inconnue a été acceptée")
	}
}

// An entry whose base name is a directory marker is skipped: nothing may be
// written outside the bundle directory, whatever the archive claims.
func TestEntryNamedDotDotIsSkipped(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"tex/latex/zz/..", "tex/latex/zz/.", "tex/latex/zz/zz.cls"} {
		w, _ := zw.Create(name)
		w.Write([]byte("x\n"))
	}
	zw.Close()
	data := buf.Bytes()
	url, _ := serveZip(t, data)
	b := testBundle(t, data, HTTPSource{URL: url, Label: "amont"})
	tree, err := Open(context.Background(), b, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Names(); len(got) != 1 || got[0] != "zz.cls" {
		t.Errorf("Names() = %v, attendu [zz.cls]", got)
	}
}

// get against a host that refuses the connection: the transport error is
// reported, not swallowed into an empty body.
func TestGetOnAnUnreachableHost(t *testing.T) {
	s := OCISource{Registry: "127.0.0.1:1", Repository: "zz", Reference: "1.0"}
	if _, err := s.get(context.Background(), http.DefaultClient, "http://127.0.0.1:1/v2/", "", "*/*"); err == nil {
		t.Error("un hôte injoignable devrait échouer")
	}
}
