# texmf — go-tex

[![ci](https://github.com/go-tex/texmf/actions/workflows/ci.yml/badge.svg)](https://github.com/go-tex/texmf/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests)

**Gets a TeX engine the support tree a document asks for, on a machine with no
TeX distribution installed.** Pure Go, no cgo, standard library only.

It redistributes nothing. Every byte of TeX macro source comes from a pinned
upstream release, fetched onto the user's own machine, checked against a
recorded SHA-256, and kept in the user cache.

That is deliberate. beamer, for one, offers itself *"under the LaTeX Project
Public License and/or under the GNU Public License"* — and a download is not a
redistribution, so nothing here has to choose between those two licences on a
user's behalf.

## Use

```go
tree, err := texmf.Open(ctx, texmf.Beamer, texmf.Options{})
if err == nil {
    opt.Resolve = tree.Resolve // github.com/go-tex/engine Options.Resolve
}
```

`Tree.Resolve` has exactly the shape the engine's `Options.Resolve` wants: a TeX
engine asks for `beamerbasetitle.sty`, never for a path.

A caller that already has the files — a TeX distribution on the machine, a
vendored copy, an air-gapped host — does not need this package at all. The
engine's own search path and `Options.Resolve` already take precedence over
anything fetched here.

## Why it is worth fetching

Measured over **10025 real beamer talks**:

| | documents rendered | pages typeset |
|---|---|---|
| with `beamer.cls` | **99.9%** | 87 209 (~8.7 per talk) |
| engine emulation only | 92.3% | 14 162 (~1.4 per talk) |

The difference is not an edge case; it is most of the content.

And natbib, for the same reason read off the text rather than the page count:
without it an engine's own emulation renders `\citep{alpha}` as `[1]` where a
reference build gives `(Author et al., 2021)` — a citation about three times too
narrow, on **68 of 154** measured arXiv papers, 20 of which load the package by
name (go-tex/engine#703). With the real package the output matches the reference
word for word, so what was missing was delivery and not typesetting.

⛔ natbib is also why this module reads `.tar.xz`. It ships on CTAN as
`natbib.dtx` + `natbib.ins` and nowhere as a ready `.sty`; a generated
`natbib.sty` exists only where docstrip has already run, which is inside a
distribution, and TeX Live's package archives are xz'd tars. The decoder is
[go-compressions/xz](https://github.com/go-compressions/xz) — pure Go, this
fleet's own, and it bounds what a stream NEEDS rather than believing the window
its header declares, which is the only safe way to decompress an archive fetched
over the network: the digest of a bomb is still a digest.

Fonts ride the same route: `texmf.Inconsolata` (answers `zi4` and `inconsolata`,
SIL OFL 1.1) is the monospaced face acmart asks for, so `\texttt` takes the same
advance as under a reference build.

## How a bundle arrives

Routes are tried in order, and **the digest is the only trust anchor** — a route
is a delivery mechanism, never an authority:

1. the **cache** (`<user cache>/go-tex/texmf/beamer@3.77`), so a second run is
   offline whatever the options say;
2. **`ghcr.io/go-tex/texmf/beamer`**, a registry this project controls, so the
   default route does not depend on a third party staying up;
3. the **upstream release**, so the bundle stays reachable when the registry is
   not.

Only the files a TeX engine opens are extracted: `tex/latex/beamer/` and nothing
else. A TDS archive's `doc/` and `source/` trees are thousands of files that
never reach the disk.

The container is **sniffed, not declared**: zip, gzipped tar and xz'd tar are
told apart by their first bytes, because which one an upstream release publishes
is a fact about that release rather than a choice — and a catalogue field every
entry had to get right is a field no entry could check.

## In a browser

`Open` extracts into the user cache, and Go's **js/wasm filesystem shim answers
`ENOSYS`** to most calls — so it cannot run in a browser, which is exactly the host
`Options.Resolve` exists to serve. `OpenInMemory` is the same fetch and the same
digest check, keeping the files in memory instead:

```go
tree, err := texmf.OpenInMemory(ctx, texmf.Beamer, texmf.Options{})
```

`OpenInMemory` still fetches, and **a browser cannot**: neither `ghcr.io` nor the
GitHub release sends an `Access-Control-Allow-Origin` header (measured), so a page
is refused by CORS. There the host fetches the bytes its own way — same-origin
next to the page, a CDN that does send the header, the Cache API, a bundled asset
— and hands them over:

```go
tree, err := texmf.FromArchive(data, texmf.Beamer)  // digest still decides
```

Under **node** or **wasip1** a real filesystem is bridged in (measured:
`os.UserCacheDir`, `os.MkdirTemp` and `os.WriteFile` all succeed under node), so
`Open` works there and is the better choice — it caches. Nothing is cached in
memory, so every `OpenInMemory` call fetches: keep the returned `Tree`.

`Options.Offline` forbids every fetch: a cached bundle is used, an absent one
returns `ErrNotCached`. That is what an air-gapped or reproducible build sets.

## Tests

`go test ./...` — **100% statement coverage**, including every filesystem and
transport failure, on six 64-bit architectures, three operating systems and both
wasm targets.

The suite never touches the network: it runs against a local `httptest` registry
and temporary directories, so it is as deterministic on a laptop as on a runner.
One test does use the network and is skipped unless `TEXMF_NETWORK` is set — CI
runs it to check the pinned digests still describe what upstream serves, so a
re-cut release is found there rather than by a user with a cold cache.

## License

BSD-3-Clause, © the go-tex/texmf authors. The material it fetches carries its
own licence, from its own publisher, onto the user's own machine.
