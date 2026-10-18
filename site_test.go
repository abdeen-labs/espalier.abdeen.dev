package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testSite is the site with no release read yet, or with release.
func testSite(t *testing.T, release *Release) *site {
	t.Helper()
	releases, err := newReleases(defaultAppcastURL, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	releases.current = release
	s, err := newSite(releases)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func get(s *site, method, target string, header map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	for name, value := range header {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	s.ServeHTTP(recorder, request)
	return recorder
}

var release = &Release{
	Version:              "1.0",
	Build:                "1",
	URL:                  "https://updates.abdeen.industries/espalier/Espalier-1.0.dmg",
	Size:                 59166933,
	Published:            time.Date(2026, 10, 17, 16, 41, 49, 0, time.UTC),
	MinimumSystemVersion: "27.0",
}

func TestPageShowsTheRelease(t *testing.T) {
	response := get(testSite(t, release), http.MethodGet, "/", nil)
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != cacheRecheck {
		t.Fatalf("status %d, cache %q", response.Code, response.Header().Get("Cache-Control"))
	}
	for _, want := range []string{`href="` + release.URL + `"`, "Version 1.0", "59 MB", "October 17, 2026", "macOS 27 or later"} {
		if !strings.Contains(body, want) {
			t.Errorf("page has no %q", want)
		}
	}
}

func TestPageWithoutAReleaseLinksToDownload(t *testing.T) {
	body := get(testSite(t, nil), http.MethodGet, "/", nil).Body.String()
	if !strings.Contains(body, `href="/download"`) || strings.Contains(body, "Version ") {
		t.Fatal("the page should link to /download and name no version")
	}
}

func TestPageFollowsANewRelease(t *testing.T) {
	s := testSite(t, release)
	newer := *release
	newer.Version, newer.Build, newer.URL = "1.1", "2", "https://updates.abdeen.industries/espalier/Espalier-1.1.dmg"
	s.releases.mu.Lock()
	s.releases.current = &newer
	s.releases.mu.Unlock()
	if body := get(s, http.MethodGet, "/", nil).Body.String(); !strings.Contains(body, "Version 1.1") || !strings.Contains(body, newer.URL) {
		t.Fatal("the page still shows the old release")
	}
}

func TestDownload(t *testing.T) {
	response := get(testSite(t, release), http.MethodGet, "/download", nil)
	if response.Code != http.StatusFound || response.Header().Get("Location") != release.URL {
		t.Fatalf("got %d to %q", response.Code, response.Header().Get("Location"))
	}
	response = get(testSite(t, nil), http.MethodGet, "/download", nil)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") == "" {
		t.Fatalf("got %d without a release", response.Code)
	}
}

func TestStaticFilesAreServedAtHashedPathsOnly(t *testing.T) {
	s := testSite(t, release)
	hashed := s.hashed["/static/site.css"]
	if hashed == "" || hashed == "/static/site.css" {
		t.Fatalf("no hashed path for site.css: %q", hashed)
	}

	response := get(s, http.MethodGet, hashed, map[string]string{"Accept-Encoding": "br, gzip"})
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != cacheForever {
		t.Fatalf("status %d, cache %q", response.Code, response.Header().Get("Cache-Control"))
	}
	if response.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("not compressed for a browser that takes gzip")
	}
	reader, err := gzip.NewReader(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	css, _ := io.ReadAll(reader)
	if !strings.Contains(string(css), ":root") {
		t.Fatal("the gzipped body isn't the stylesheet")
	}

	etag := response.Header().Get("ETag")
	if response := get(s, http.MethodGet, hashed, map[string]string{"If-None-Match": etag}); response.Code != http.StatusNotModified {
		t.Fatalf("got %d for a matching ETag", response.Code)
	}
	if response := get(s, http.MethodGet, "/static/site.css", nil); response.Code != http.StatusNotFound {
		t.Fatalf("got %d for the unhashed path", response.Code)
	}
}

func TestRootFilesAndMissingPages(t *testing.T) {
	s := testSite(t, release)
	if response := get(s, http.MethodGet, "/robots.txt", nil); response.Code != http.StatusOK || response.Header().Get("Cache-Control") != cacheHour {
		t.Fatalf("robots.txt: %d, %q", response.Code, response.Header().Get("Cache-Control"))
	}
	response := get(s, http.MethodGet, "/nope", nil)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `<main class="missing">`) {
		t.Fatalf("got %d for a missing page", response.Code)
	}
}

func TestHeadSendsNoBody(t *testing.T) {
	response := get(testSite(t, release), http.MethodHead, "/", nil)
	if response.Code != http.StatusOK || response.Body.Len() != 0 || response.Header().Get("Content-Length") == "0" {
		t.Fatalf("HEAD: %d, %d bytes, length %q", response.Code, response.Body.Len(), response.Header().Get("Content-Length"))
	}
}

func TestOtherMethodsAreRefused(t *testing.T) {
	response := get(testSite(t, release), http.MethodPost, "/", nil)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST: %d, Allow %q", response.Code, response.Header().Get("Allow"))
	}
}

func TestEveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	s := testSite(t, release)
	for _, target := range []string{"/", "/download", "/nope", "/healthz"} {
		header := get(s, http.MethodGet, target, nil).Header()
		if !strings.Contains(header.Get("Content-Security-Policy"), "default-src 'none'") || header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: missing security headers", target)
		}
	}
}

func TestAcceptsGzip(t *testing.T) {
	for header, want := range map[string]bool{
		"":                   false,
		"gzip":               true,
		"br, gzip;q=0.8":     true,
		"gzip;q=0":           false,
		"GZIP":               true,
		"*":                  true,
		"identity":           false,
		"deflate, gzip; q=0": false,
	} {
		if got := acceptsGzip(header); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}
