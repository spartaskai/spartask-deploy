package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLatestStable(t *testing.T) {
	cases := map[string][]string{
		"1.4.1":  {"1.4.0", "1.4.1", "1.5.0-rc1", "latest", "sha-abc"},
		"1.10.0": {"1.9.9", "v1.10.0", "1.2.30"},
		"":       {"latest", "main", "2.0.0-beta"},
	}
	for want, tags := range cases {
		if got := latestStable(tags); got != want {
			t.Errorf("%v: got %q, want %q", tags, got, want)
		}
	}
}

// A public image's tags are read through the registry's anonymous token, across pages.
func TestRegistryTags(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if r.URL.Query().Get("scope") != "repository:spartaskai/spartask:pull" || r.URL.Query().Get("service") != "test" {
				http.Error(w, "bad scope", http.StatusBadRequest)
				return
			}
			w.Write([]byte(`{"token":"anon"}`))
		case r.Header.Get("Authorization") != "Bearer anon":
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="test",scope="repository:spartaskai/spartask:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Query().Get("last") == "":
			w.Header().Set("Link", `</v2/spartaskai/spartask/tags/list?last=1.4.0&n=1000>; rel="next"`)
			w.Write([]byte(`{"name":"spartaskai/spartask","tags":["1.3.0","1.4.0"]}`))
		default:
			w.Write([]byte(`{"name":"spartaskai/spartask","tags":["1.4.1","latest"]}`))
		}
	}))
	defer srv.Close()
	previous := http.DefaultClient
	http.DefaultClient = srv.Client()
	defer func() { http.DefaultClient = previous }()

	image := strings.TrimPrefix(srv.URL, "https://") + "/spartaskai/spartask"
	tags, err := registryTags(context.Background(), image)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tags, ",") != "1.3.0,1.4.0,1.4.1,latest" {
		t.Fatalf("tags: %v", tags)
	}
	if latest, err := latestRelease(context.Background(), registryTags, image); err != nil || latest != "1.4.1" {
		t.Fatalf("latest: %q %v", latest, err)
	}
	if _, err := registryTags(context.Background(), "spartask"); err == nil {
		t.Fatal("an image without its registry was accepted")
	}
}
