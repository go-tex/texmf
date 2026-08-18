// Copyright (c) the go-tex/texmf authors.
// SPDX-License-Identifier: BSD-3-Clause

package texmf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OCISource pulls an archive stored as a single-layer OCI artifact — the shape a
// registry the operator controls can serve, so the default route to a bundle is
// theirs rather than a third party's.
//
// It speaks just enough of the distribution API to pull one blob: anonymous
// token auth, one manifest, one layer. Pulling one artifact does not need a
// registry client library, and not depending on one keeps this module's
// dependencies to the standard library.
type OCISource struct {
	// Registry is the host ("ghcr.io").
	Registry string
	// Repository is the path within it ("go-tex/texmf/beamer").
	Repository string
	// Reference is a tag ("3.77") or a digest ("sha256:…").
	Reference string
	// Label names the route in messages. Empty builds one from the coordinates.
	Label string
	// Client is used when set; nil means a client with a sane timeout.
	Client *http.Client
}

// Describe implements Source.
func (s OCISource) Describe() string {
	if s.Label != "" {
		return s.Label
	}
	return fmt.Sprintf("OCI artifact %s/%s:%s", s.Registry, s.Repository, s.Reference)
}

type ociManifest struct {
	Layers []struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"layers"`
}

const (
	mediaManifest     = "application/vnd.oci.image.manifest.v1+json"
	mediaDockerV2     = "application/vnd.docker.distribution.manifest.v2+json"
	mediaManifestList = "application/vnd.oci.image.index.v1+json"
)

// Fetch implements Source: resolve the manifest, then pull its single layer.
func (s OCISource) Fetch(ctx context.Context) ([]byte, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	return s.fetchVia(ctx, client, "https://"+s.Registry)
}

// fetchVia is Fetch with the client and the scheme+host supplied, which is what
// lets the tests run the whole registry conversation — token, manifest, blob —
// against a real local server rather than a stub of this package's own code.
func (s OCISource) fetchVia(ctx context.Context, client *http.Client, base string) ([]byte, error) {
	token, err := s.token(ctx, client, base)
	if err != nil {
		return nil, err
	}

	body, err := s.get(ctx, client, base+"/v2/"+s.Repository+"/manifests/"+s.Reference, token,
		strings.Join([]string{mediaManifest, mediaDockerV2, mediaManifestList}, ", "))
	if err != nil {
		return nil, err
	}
	var m ociManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if len(m.Layers) != 1 {
		return nil, fmt.Errorf("expected exactly one layer, got %d", len(m.Layers))
	}

	return s.get(ctx, client, base+"/v2/"+s.Repository+"/blobs/"+m.Layers[0].Digest, token, "*/*")
}

// token asks the registry for an anonymous pull token. A registry that needs
// none answers the request anyway or refuses it, and an empty token is then
// simply not sent.
func (s OCISource) token(ctx context.Context, client *http.Client, base string) (string, error) {
	u := base + "/token?scope=" +
		url.QueryEscape("repository:"+s.Repository+":pull") +
		"&service=" + url.QueryEscape(s.Registry)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil // no anonymous token here; try the pull unauthenticated
	}
	var t struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t); err != nil {
		return "", nil
	}
	if t.Token != "" {
		return t.Token, nil
	}
	return t.AccessToken, nil
}

func (s OCISource) get(ctx context.Context, client *http.Client, u, token, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArchiveSize {
		return nil, fmt.Errorf("GET %s: response is larger than %d bytes", u, maxArchiveSize)
	}
	return data, nil
}
