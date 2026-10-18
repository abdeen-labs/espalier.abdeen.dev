package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const appcastHead = `<?xml version="1.0" standalone="yes"?>
<rss xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle" version="2.0"><channel><title>Espalier</title>`

func item(build, version, link string, extra string) string {
	return `<item><title>` + version + `</title><pubDate>Sat, 17 Oct 2026 16:41:49 -0700</pubDate>` +
		`<sparkle:version>` + build + `</sparkle:version><sparkle:shortVersionString>` + version + `</sparkle:shortVersionString>` +
		`<sparkle:minimumSystemVersion>27.0</sparkle:minimumSystemVersion>` + extra +
		`<enclosure url="` + link + `" length="59166933" type="application/octet-stream" sparkle:edSignature="x"/></item>`
}

func appcast(items ...string) string {
	return appcastHead + strings.Join(items, "") + `</channel></rss><!-- sparkle-signatures: x -->`
}

var feedBase, _ = url.Parse("https://updates.abdeen.industries/espalier/appcast.xml")

func TestParseAppcastPicksTheNewestReleaseOnTheDefaultChannel(t *testing.T) {
	feed := appcast(
		item("9", "1.9", "https://updates.abdeen.industries/espalier/Espalier-1.9.dmg", ""),
		item("10", "1.10", "https://updates.abdeen.industries/espalier/Espalier-1.10.dmg", ""),
		item("11", "1.11 beta", "https://updates.abdeen.industries/espalier/Espalier-1.11b.dmg", "<sparkle:channel>beta</sparkle:channel>"),
		item("12", "1.12", "https://example.com/espalier/Espalier-1.12.dmg", ""),
	)
	release, err := parseAppcast(strings.NewReader(feed), feedBase)
	if err != nil {
		t.Fatal(err)
	}
	want := Release{
		Version:              "1.10",
		Build:                "10",
		URL:                  "https://updates.abdeen.industries/espalier/Espalier-1.10.dmg",
		Size:                 59166933,
		Published:            time.Date(2026, 10, 17, 16, 41, 49, 0, time.FixedZone("", -7*3600)),
		MinimumSystemVersion: "27.0",
	}
	if release.Version != want.Version || release.Build != want.Build || release.URL != want.URL ||
		release.Size != want.Size || !release.Published.Equal(want.Published) || release.MinimumSystemVersion != want.MinimumSystemVersion {
		t.Fatalf("got %+v, want %+v", release, want)
	}
}

func TestParseAppcastReadsVersionsOnTheEnclosure(t *testing.T) {
	feed := appcastHead + `<item><enclosure url="https://updates.abdeen.industries/espalier/Espalier-1.0.dmg" length="10" ` +
		`sparkle:version="1" sparkle:shortVersionString="1.0"/></item></channel></rss>`
	release, err := parseAppcast(strings.NewReader(feed), feedBase)
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "1.0" || release.Build != "1" || !release.Published.IsZero() {
		t.Fatalf("got %+v", release)
	}
}

func TestParseAppcastFailsWithoutAUsableRelease(t *testing.T) {
	for name, feed := range map[string]string{
		"no items":          appcast(),
		"only another host": appcast(item("1", "1.0", "https://example.com/espalier/Espalier-1.0.dmg", "")),
		"not XML":           "<html>",
	} {
		if _, err := parseAppcast(strings.NewReader(feed), feedBase); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestIsUnderAllowsOnlyTheFeedsFolderOverHTTPS(t *testing.T) {
	for link, want := range map[string]bool{
		"https://updates.abdeen.industries/espalier/Espalier-1.0.dmg":          true,
		"http://updates.abdeen.industries/espalier/Espalier-1.0.dmg":           false,
		"https://updates.abdeen.industries/frost/Frost-2.2.3.dmg":              false,
		"https://updates.abdeen.industries/espalier/../frost/Frost.dmg":        false,
		"https://evil.example/espalier/Espalier-1.0.dmg":                       false,
		"https://user@updates.abdeen.industries/espalier/Espalier-1.0.dmg":     false,
		"https://updates.abdeen.industries.evil.example/espalier/Espalier.dmg": false,
	} {
		if got := isUnder(link, feedBase); got != want {
			t.Errorf("isUnder(%q) = %v, want %v", link, got, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1", "1", 0},
		{"2", "10", -1},
		{"1.10", "1.9", 1},
		{"1.2", "1.2.1", -1},
		{"14", "13.9", 1},
		{"1.0b2", "1.0b10", 1}, // text parts compare as text
	} {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestRefreshKeepsTheLastReleaseWhenAReadFails(t *testing.T) {
	status := http.StatusOK
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(appcast(item("1", "1.0", server.URL+"/espalier/Espalier-1.0.dmg", ""))))
	}))
	defer server.Close()

	releases, err := newReleases(server.URL+"/espalier/appcast.xml", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := releases.Latest(); ok {
		t.Fatal("a release before any read")
	}
	if err := releases.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	status = http.StatusForbidden
	if err := releases.Refresh(context.Background()); err == nil {
		t.Fatal("no error for a 403")
	}
	release, ok := releases.Latest()
	if !ok || release.Version != "1.0" {
		t.Fatalf("got %+v, %v after a failed read", release, ok)
	}
}

func TestRefreshRefusesAnOversizedAppcast(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat(" ", maxAppcastSize+1)))
	}))
	defer server.Close()
	releases, _ := newReleases(server.URL+"/espalier/appcast.xml", server.Client())
	if err := releases.Refresh(context.Background()); err == nil {
		t.Fatal("no error for an oversized appcast")
	}
}
