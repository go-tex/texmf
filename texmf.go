// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

// Package texmf gets a TeX engine the support tree a document asks for, on a
// machine with no TeX distribution installed.
//
// It redistributes nothing. Every byte of TeX macro source comes from a pinned
// upstream release, fetched onto the user's own machine, checked against a
// recorded SHA-256, and kept in the user cache. That is deliberate: beamer, for
// one, is offered "under the LaTeX Project Public License and/or under the GNU
// Public License", and downloading it is not a redistribution, so the choice
// between those two licences never has to be made on the user's behalf.
//
// The result plugs straight into the engine:
//
//	tree, err := texmf.Open(ctx, texmf.Beamer, texmf.Options{})
//	if err == nil {
//		opt.Resolve = tree.Resolve
//	}
//
// A caller that already has the files — a TeX distribution on the machine, a
// vendored copy, an air-gapped host — does not need this package at all: the
// engine's own search path and Options.Resolve already take precedence over
// anything fetched here.
package texmf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// A Bundle names one upstream release and says which of its files a TeX engine
// should be able to see. It is a value, not a fetch: nothing happens until Open.
type Bundle struct {
	// Name and Version identify the bundle, and together form its cache
	// directory ("beamer@3.77"). Version is the upstream release's own version,
	// so a cache entry is unambiguous.
	Name    string
	Version string

	// Sources are tried in order until one yields the archive. The first is
	// normally a registry the operator controls; the last should be the upstream
	// release, so the bundle stays reachable when the registry is not.
	Sources []Source

	// SHA256 is the archive's digest, lowercase hex. Every source must produce
	// exactly these bytes — a source is a delivery route, never a trust anchor.
	SHA256 string

	// Prefix is the archive directory holding the TeX macro files; only entries
	// under it are extracted. Everything else in a TDS archive (documentation,
	// sources, PDFs) is thousands of files the engine will never open.
	Prefix string
}

// A Source is one route to a bundle's archive bytes. Fetch must return the
// complete archive; the digest is checked by the caller, so a Source is never
// trusted to validate itself.
type Source interface {
	// Describe names the route for error messages ("upstream release
	// josephwright/beamer v3.77").
	Describe() string
	// Fetch returns the archive bytes, or an error.
	Fetch(ctx context.Context) ([]byte, error)
}

// Options configures Open.
type Options struct {
	// CacheDir overrides where extracted bundles live. Empty means
	// <user cache>/go-tex/texmf, which is what the CLI wants.
	CacheDir string

	// Offline forbids every network fetch: a bundle already in the cache is
	// used, and a bundle that is not returns ErrNotCached. This is what an
	// air-gapped or reproducible build sets.
	Offline bool

	// Log, when set, is called with one line per notable step (which source was
	// tried, what it produced). Nil discards them. The CLI wires this to its own
	// stderr so a download is never silent.
	Log func(string)
}

// ErrNotCached is returned by Open when the bundle is absent from the cache and
// Options.Offline forbids fetching it.
var ErrNotCached = errors.New("texmf: bundle not cached and offline")

// A Tree is an extracted bundle. Resolve answers the engine's file lookups from
// it, and is safe for concurrent use.
type Tree struct {
	dir   string
	mu    sync.Mutex
	cache map[string][]byte
	names map[string]string // base name → path on disk
}

// Dir is where the tree was extracted. Useful for a caller that would rather put
// it on TEXINPUTS than go through Resolve.
func (t *Tree) Dir() string { return t.dir }

// Resolve returns the bytes of one file by its base name, which is how a TeX
// engine asks: "beamerbasetitle.sty", not a path. It matches the signature of
// the engine's Options.Resolve.
func (t *Tree) Resolve(name string) ([]byte, bool) {
	base := name
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if data, ok := t.cache[base]; ok {
		return data, true
	}
	path, ok := t.names[base]
	if !ok {
		return nil, false
	}
	data, err := osReadFile(path)
	if err != nil {
		return nil, false
	}
	t.cache[base] = data
	return data, true
}

// Names lists the base names the tree can answer for, sorted. Mostly for tests
// and for a caller reporting what it got.
func (t *Tree) Names() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.names))
	for n := range t.names {
		out = append(out, n)
	}
	sortStrings(out)
	return out
}

// Open makes the bundle available, fetching and extracting it only if the cache
// does not already hold it. The returned Tree reads from disk lazily.
func Open(ctx context.Context, b Bundle, opt Options) (*Tree, error) {
	dir, err := bundleDir(b, opt)
	if err != nil {
		return nil, err
	}
	if ok, err := isPopulated(dir); err != nil {
		return nil, err
	} else if !ok {
		if opt.Offline {
			return nil, fmt.Errorf("%w: %s@%s", ErrNotCached, b.Name, b.Version)
		}
		if err := fetchInto(ctx, b, dir, opt); err != nil {
			return nil, err
		}
	}
	return openTree(dir)
}

// bundleDir is where this exact bundle version lives.
func bundleDir(b Bundle, opt Options) (string, error) {
	root := opt.CacheDir
	if root == "" {
		base, err := osUserCacheDir()
		if err != nil {
			return "", fmt.Errorf("texmf: no user cache directory: %w", err)
		}
		root = filepath.Join(base, "go-tex", "texmf")
	}
	return filepath.Join(root, b.Name+"@"+b.Version), nil
}

// isPopulated reports whether a bundle directory holds a completed extraction.
// The marker is written last, so an extraction interrupted half way is not
// mistaken for a good one.
func isPopulated(dir string) (bool, error) {
	_, err := osStat(filepath.Join(dir, ".complete"))
	switch {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, err
	}
}

// fetchInto tries each source in turn, checks the digest, and extracts.
func fetchInto(ctx context.Context, b Bundle, dir string, opt Options) error {
	logf(opt, "texmf: %s@%s is not cached", b.Name, b.Version)
	if len(b.Sources) == 0 {
		return fmt.Errorf("texmf: %s@%s has no sources", b.Name, b.Version)
	}
	var errs []error
	for _, src := range b.Sources {
		logf(opt, "texmf: trying %s", src.Describe())
		data, err := src.Fetch(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", src.Describe(), err))
			logf(opt, "texmf: %s failed: %v", src.Describe(), err)
			continue
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != b.SHA256 {
			err := fmt.Errorf("digest is %s, expected %s", got, b.SHA256)
			errs = append(errs, fmt.Errorf("%s: %w", src.Describe(), err))
			logf(opt, "texmf: %s failed: %v", src.Describe(), err)
			continue
		}
		n, err := extractZip(data, b.Prefix, dir)
		if err != nil {
			return fmt.Errorf("texmf: extracting %s@%s: %w", b.Name, b.Version, err)
		}
		logf(opt, "texmf: %s@%s ready, %d files in %s", b.Name, b.Version, n, dir)
		return nil
	}
	return fmt.Errorf("texmf: no source yielded %s@%s: %w", b.Name, b.Version, errors.Join(errs...))
}

func logf(opt Options, format string, a ...any) {
	if opt.Log != nil {
		opt.Log(fmt.Sprintf(format, a...))
	}
}

// sortStrings is a tiny insertion sort, kept local so the package's only
// dependencies stay the standard library and (for the signed source) go-attest.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
