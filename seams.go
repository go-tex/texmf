// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import "os"

// The filesystem calls this package makes go through these variables so a test
// can make each one fail. Every one of them guards a real failure a user can
// meet — a full disk, a read-only cache, a home directory the process cannot
// read — and an error path that has never been executed is an error path that
// has never been shown to work.
//
// They are variables, not an interface: the production code reads exactly like
// the standard library, and a test overrides one for the length of one case.
var (
	osMkdirAll     = os.MkdirAll
	osMkdirTemp    = os.MkdirTemp
	osWriteFile    = os.WriteFile
	osRemoveAll    = os.RemoveAll
	osRename       = os.Rename
	osReadDir      = os.ReadDir
	osReadFile     = os.ReadFile
	osStat         = os.Stat
	osUserCacheDir = os.UserCacheDir
)
