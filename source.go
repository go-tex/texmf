// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPSource fetches an archive from one URL. It is the plain route: a release
// asset published by the upstream project, pinned to an exact version so the
// bytes never move under us.
type HTTPSource struct {
	// URL is fetched with GET, following redirects.
	URL string
	// Label names the route in messages ("upstream release josephwright/beamer
	// v3.77"). Empty falls back to the URL.
	Label string
	// Client is used when set; nil means a client with a sane timeout.
	Client *http.Client
}

// Describe implements Source.
func (s HTTPSource) Describe() string {
	if s.Label != "" {
		return s.Label
	}
	return s.URL
}

// maxArchiveSize bounds a download. A TDS archive is a few megabytes; this is
// what stops a redirect to something enormous from filling the disk before the
// digest can reject it.
const maxArchiveSize = 64 << 20

// Fetch implements Source.
func (s HTTPSource) Fetch(ctx context.Context) ([]byte, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", s.URL, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArchiveSize {
		return nil, fmt.Errorf("GET %s: response is larger than %d bytes", s.URL, maxArchiveSize)
	}
	return data, nil
}
