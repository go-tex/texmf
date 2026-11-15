// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"bytes"
	"fmt"

	"github.com/go-compressions/xz"
)

// xzMagic is the .xz stream header: 0xFD "7zXZ" 0x00
// (xz-file-format.txt §2.1.1.1, tukaani.org).
var xzMagic = []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}

// isXZ sniffs the archive, for the same reason isGzip does: which container an
// upstream release publishes is a fact about that release, not a choice, and a
// catalogue field every entry had to get right is a field no entry could check.
//
// TeX Live's own package archives are the reason this exists. natbib ships on
// CTAN as .dtx + .ins and nowhere as a ready .sty, so the only archive carrying
// a docstrip-generated natbib.sty is TeX Live's runtime package, which is
// .tar.xz.
func isXZ(archive []byte) bool {
	return bytes.HasPrefix(archive, xzMagic)
}

// maxDictionary is the ceiling on the LZMA2 dictionary window readTarXZ will
// let a stream allocate. It is a var, not a const, so a test can lower it and
// prove the ceiling is wired rather than take the production value on trust.
//
// maxArchiveSize is the right value because a window wider than the largest
// archive this module will download cannot be needed to decode one. Measured
// 2026-11-15, the archive the natbib bundle pins declares `--lzma2=dict=8MiB`
// ("Memory needed: 9 MiB"), a factor of eight under it.
var maxDictionary = maxArchiveSize

// readTarXZ reads every entry under one of the prefixes, exactly as readTarGz
// and readZip do and under the same bounds: files over maxFileSize are refused,
// .lua is dropped, and the result is FLATTENED because the engine asks for
// "natbib.sty" and never for a path.
//
// ⛔ THE DICTIONARY IS BOUNDED, AND THAT IS THE WHOLE REASON THIS READER CAN
// EXIST HERE. An .xz stream declares the dictionary window its blocks need, and
// a decoder that believes the declaration allocates whatever a crafted header
// asks for — before a single byte of output exists, so neither the LimitReader
// below nor the digest check upstream can see it. The digest of a bomb is still
// a digest. go-compressions/xz allocates the window lazily and never past
// ReaderConfig.MaxDictionary, failing with xz.ErrTooLarge instead; its package
// doc is "whose memory follows the decoded data and not the sizes a header
// declares", and the ceiling is maxDictionary.
//
// ⚠ NOTE WHAT THAT DOES AND DOES NOT MEAN, because it is easy to assert the
// wrong thing: the ceiling bounds what a stream NEEDS, not what it DECLARES. A
// small archive whose header announces a 256 MiB window decodes fine and
// allocates almost nothing — that is the property, not a hole in it. Only a
// stream whose data actually asks for more than the ceiling is refused, and
// then before the allocation.
//
// The stream is also read through a LimitReader, as in readTarGz, so an archive
// that decompresses far beyond its own size is refused before it is written
// anywhere.
func readTarXZ(archive []byte, prefixes []string) (map[string][]byte, error) {
	zr, err := xz.ReaderConfig{MaxDictionary: maxDictionary}.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("reading xz: %w", err)
	}
	return readTarEntries(zr, prefixes)
}
