package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SparTask releases are the image's tags in its registry. install and update offer the newest
// stable one and write that exact version to .env, so an installation always runs a known release.

// maxTagPages bounds the pages of a registry's tag list that are read.
const maxTagPages = 20

// stableVersionPattern is a release tag without a pre-release part (1.4.1 or v1.4.1, not 1.5.0-rc1).
var stableVersionPattern = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)$`)

// latestStable returns the newest stable version among tags, without a "v" prefix; "" when none.
func latestStable(tags []string) string {
	var best [3]int
	found := ""
	for _, tag := range tags {
		m := stableVersionPattern.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		var v [3]int
		for i := range v {
			n, err := strconv.Atoi(m[i+1])
			if err != nil {
				n = -1 // a number too large for a real release
			}
			v[i] = n
		}
		if v[0] < 0 || v[1] < 0 || v[2] < 0 {
			continue
		}
		if found == "" || v[0] > best[0] || (v[0] == best[0] && (v[1] > best[1] || (v[1] == best[1] && v[2] > best[2]))) {
			best, found = v, fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
		}
	}
	return found
}

// latestRelease returns the newest stable release of image ("registry/repository") from its
// registry: the image is public, so the registry's anonymous pull token is enough.
func latestRelease(ctx context.Context, tags func(context.Context, string) ([]string, error), image string) (string, error) {
	list, err := tags(ctx, image)
	if err != nil {
		return "", err
	}
	if v := latestStable(list); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s has no release tag", image)
}

// offerLatest fills an unset version with the newest release; when the registry cannot be read
// it keeps the version this spartaskctl was built with and says why.
func offerLatest(ctx context.Context, tags func(context.Context, string) ([]string, error), image, version string) string {
	if version != "" {
		return version
	}
	latest, err := latestRelease(ctx, tags, image)
	if err != nil {
		fmt.Printf("The latest SparTask release could not be read (%v)", err)
		if defaultVersion != "" {
			fmt.Printf("; offering %s, the release this package was built with.\n", defaultVersion)
		} else {
			fmt.Println("; enter the version.")
		}
		return defaultVersion
	}
	fmt.Printf("Latest SparTask release: %s\n", latest)
	return latest
}

// registryTags lists the tags of a public image through the registry API (Docker Registry HTTP
// API v2): the first request names the token service in its WWW-Authenticate challenge.
func registryTags(ctx context.Context, image string) ([]string, error) {
	host, repo, ok := strings.Cut(image, "/")
	if !ok || !strings.ContainsAny(host, ".:") {
		return nil, fmt.Errorf("image %q does not name its registry (e.g. ghcr.io/owner/name)", image)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	next := "https://" + host + "/v2/" + repo + "/tags/list?n=1000"
	token := ""
	var all []string
	for page := 0; next != "" && page < maxTagPages; page++ {
		resp, err := registryGet(ctx, next, token)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && token == "" {
			challenge := resp.Header.Get("WWW-Authenticate")
			resp.Body.Close()
			if token, err = registryToken(ctx, challenge); err != nil {
				return nil, err
			}
			page--
			continue
		}
		var body struct {
			Tags []string `json:"tags"`
		}
		err = decodeRegistry(resp, &body)
		if err != nil {
			return nil, fmt.Errorf("%s tags: %w", image, err)
		}
		all = append(all, body.Tags...)
		if next, err = nextPage(next, resp.Header.Get("Link")); err != nil {
			return nil, err
		}
	}
	return all, nil
}

func registryGet(ctx context.Context, address, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(req)
}

// decodeRegistry reads a JSON answer and closes it; any status other than 200 is an error.
func decodeRegistry(resp *http.Response, v any) error {
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(data, v)
}

// registryToken gets an anonymous pull token from the service a Bearer challenge names
// (realm="https://ghcr.io/token",service="ghcr.io",scope="repository:owner/name:pull").
func registryToken(ctx context.Context, challenge string) (string, error) {
	scheme, params, _ := strings.Cut(challenge, " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return "", fmt.Errorf("the registry asks for %q authentication", scheme)
	}
	values := url.Values{}
	realm := ""
	for _, part := range splitChallenge(params) {
		key, value, _ := strings.Cut(part, "=")
		value = strings.Trim(value, `"`)
		if key == "realm" {
			realm = value
		} else if key == "service" || key == "scope" {
			values.Set(key, value)
		}
	}
	u, err := url.Parse(realm)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("the registry names an invalid token service %q", realm)
	}
	u.RawQuery = values.Encode()
	resp, err := registryGet(ctx, u.String(), "")
	if err != nil {
		return "", err
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := decodeRegistry(resp, &body); err != nil {
		return "", fmt.Errorf("registry token: %w", err)
	}
	if body.Token != "" {
		return body.Token, nil
	}
	if body.AccessToken != "" {
		return body.AccessToken, nil
	}
	return "", errors.New("registry token: empty answer")
}

// splitChallenge splits the comma-separated parameters of a challenge, keeping commas inside
// quoted values.
func splitChallenge(params string) []string {
	var parts []string
	quoted, start := false, 0
	for i, r := range params {
		switch {
		case r == '"':
			quoted = !quoted
		case r == ',' && !quoted:
			parts = append(parts, strings.TrimSpace(params[start:i]))
			start = i + 1
		}
	}
	return append(parts, strings.TrimSpace(params[start:]))
}

// nextPage resolves the rel="next" Link of a tag list page against the current address; "" at
// the last page.
func nextPage(current, link string) (string, error) {
	target, rest, ok := strings.Cut(link, ">")
	if !ok || !strings.Contains(rest, `rel="next"`) {
		return "", nil
	}
	base, err := url.Parse(current)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(strings.TrimPrefix(strings.TrimSpace(target), "<"))
	if err != nil {
		return "", fmt.Errorf("registry Link header: %w", err)
	}
	return base.ResolveReference(ref).String(), nil
}
