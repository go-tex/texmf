// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
)

// maxFileSize bounds one extracted file. TeX macro files are tens of kilobytes;
// anything past this is not a macro file and the archive is not what we think it
// is. Without the bound a hostile archive could decompress to arbitrary size,
// and a digest check does not help — the digest of a zip bomb is still a digest.
const maxFileSize = 8 << 20

// extractZip writes the archive's files under prefix into dir, FLATTENED to
// their base names, and returns how many it wrote.
//
// Flattening is what the engine wants: TeX asks for "beamerbasetitle.sty", never
// for a path, so a nested tree would only have to be walked again at every
// lookup. It also means a TDS archive's doc/ and source/ trees — thousands of
// files, none of which the engine opens — never reach the disk.
//
// The extraction goes to a temporary directory and is renamed into place, and
// the .complete marker is written last, so an interrupted run leaves nothing
// that a later one would mistake for a finished bundle.
func extractZip(archive []byte, prefix, dir string) (int, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return 0, fmt.Errorf("reading zip: %w", err)
	}
	if err := osMkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return 0, err
	}
	tmp, err := osMkdirTemp(filepath.Dir(dir), ".tmp-"+filepath.Base(dir)+"-")
	if err != nil {
		return 0, err
	}
	defer osRemoveAll(tmp)

	n := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		base := path.Base(f.Name)
		if base == "" || base == "." || base == ".." {
			continue
		}
		data, err := readZipEntry(f)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", f.Name, err)
		}
		// filepath.Base again on the joined path so that no entry name, however
		// crafted, can write outside tmp.
		if err := osWriteFile(filepath.Join(tmp, filepath.Base(base)), data, 0o644); err != nil {
			return 0, err
		}
		n++
	}
	if n == 0 {
		return 0, fmt.Errorf("no entries under %q", prefix)
	}
	if err := osWriteFile(filepath.Join(tmp, ".complete"), []byte(prefix+"\n"), 0o644); err != nil {
		return 0, err
	}
	if err := osRemoveAll(dir); err != nil {
		return 0, err
	}
	if err := osRename(tmp, dir); err != nil {
		return 0, err
	}
	return n, nil
}

// readZipEntry reads one entry, refusing anything over maxFileSize.
func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("entry is larger than %d bytes", maxFileSize)
	}
	return data, nil
}

// openTree indexes an extracted directory.
func openTree(dir string) (*Tree, error) {
	entries, err := osReadDir(dir)
	if err != nil {
		return nil, err
	}
	t := &Tree{dir: dir, cache: map[string][]byte{}, names: map[string]string{}}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		t.names[e.Name()] = filepath.Join(dir, e.Name())
	}
	return t, nil
}
