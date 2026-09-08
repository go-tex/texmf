// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
)

// A bundle's archive is a zip or a gzipped tar, and which one is a fact about
// the upstream release rather than a choice: beamer and pgf publish TDS zips,
// and Linux Libertine publishes only LinLibertineOTF_<version>.tgz. Sniffing the
// first bytes keeps that out of the catalogue, where it would be a field every
// entry had to get right and no entry could check.
func isGzip(archive []byte) bool {
	return len(archive) >= 2 && archive[0] == 0x1f && archive[1] == 0x8b
}

// readTarGz reads every entry under one of the prefixes into a map keyed by base
// name, exactly as readZip does and under the same bounds: files over
// maxFileSize are refused, .lua is dropped, and the result is FLATTENED because
// the engine asks for "LinLibertine_R.otf" and never for a path.
//
// The whole archive is decompressed through a LimitReader as well as each entry,
// so an archive that decompresses far beyond its own size is refused before it
// is written anywhere. A digest check does not help here — the digest of a zip
// bomb is still a digest.
func readTarGz(archive []byte, prefixes []string) (map[string][]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("reading gzip: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(io.LimitReader(zr, maxArchiveSize+1))
	out := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar: %w", err)
		}
		if h.Typeflag != tar.TypeReg || !underAny(h.Name, prefixes) {
			continue
		}
		if path.Ext(h.Name) == ".lua" {
			continue
		}
		base := path.Base(h.Name)
		if base == "" || base == "." || base == ".." {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxFileSize+1))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", h.Name, err)
		}
		if len(data) > maxFileSize {
			return nil, fmt.Errorf("%s: entry is larger than %d bytes", h.Name, maxFileSize)
		}
		out[base] = data
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no entries under %q", prefixes)
	}
	return out, nil
}

// readArchive reads a zip or a gzipped tar, whichever the bytes are.
func readArchive(archive []byte, prefixes []string) (map[string][]byte, error) {
	if isGzip(archive) {
		return readTarGz(archive, prefixes)
	}
	return readZip(archive, prefixes)
}
