package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// Espalier's feed: the same appcast Sparkle reads in the app.
	defaultAppcastURL = "https://updates.abdeen.industries/espalier/appcast.xml"
	// How often the feed is read again once a read has worked.
	refreshInterval = 10 * time.Minute
	// How long to wait before trying again after a read failed.
	retryInterval = 30 * time.Second
	// More than any appcast needs; a larger body is refused.
	maxAppcastSize = 2 << 20
)

// Release is the newest release an appcast lists.
type Release struct {
	// Version is CFBundleShortVersionString, "1.0".
	Version string
	// Build is CFBundleVersion, which orders releases.
	Build string
	// URL is the DMG's.
	URL string
	// Size is the DMG's, in bytes.
	Size int64
	// Published is the item's pubDate; zero when it has none.
	Published time.Time
	// MinimumSystemVersion is the macOS the release needs, "27.0".
	MinimumSystemVersion string
}

type appcastDocument struct {
	Items []appcastItem `xml:"channel>item"`
}

type appcastItem struct {
	PubDate              string `xml:"pubDate"`
	Version              string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle version"`
	ShortVersion         string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle shortVersionString"`
	MinimumSystemVersion string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle minimumSystemVersion"`
	Channel              string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle channel"`
	Enclosure            struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
		// Older appcasts put the versions on the enclosure.
		Version      string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle version,attr"`
		ShortVersion string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle shortVersionString,attr"`
	} `xml:"enclosure"`
}

// parseAppcast returns the newest release on the default channel whose DMG
// is under base (the appcast's own folder on the update host), so a feed can
// only send people to the update host.
func parseAppcast(r io.Reader, base *url.URL) (Release, error) {
	var document appcastDocument
	if err := xml.NewDecoder(r).Decode(&document); err != nil {
		return Release{}, fmt.Errorf("reading the appcast: %w", err)
	}
	var newest *Release
	for _, item := range document.Items {
		if item.Channel != "" {
			continue
		}
		release := Release{
			Version:              firstNonEmpty(item.ShortVersion, item.Enclosure.ShortVersion),
			Build:                firstNonEmpty(item.Version, item.Enclosure.Version),
			URL:                  item.Enclosure.URL,
			Size:                 item.Enclosure.Length,
			MinimumSystemVersion: strings.TrimSpace(item.MinimumSystemVersion),
		}
		if release.Version == "" {
			release.Version = release.Build
		}
		if release.Build == "" || release.URL == "" || !isUnder(release.URL, base) {
			continue
		}
		if published, err := time.Parse(time.RFC1123Z, strings.TrimSpace(item.PubDate)); err == nil {
			release.Published = published
		}
		if newest == nil || compareVersions(release.Build, newest.Build) > 0 {
			newest = &release
		}
	}
	if newest == nil {
		return Release{}, errors.New("the appcast lists no release")
	}
	return *newest, nil
}

// isUnder reports whether link is an https URL in base's folder on base's
// host.
func isUnder(link string, base *url.URL) bool {
	parsed, err := url.Parse(link)
	if err != nil || parsed.Scheme != "https" || parsed.Host != base.Host || parsed.User != nil {
		return false
	}
	folder := base.Path[:strings.LastIndex(base.Path, "/")+1]
	return strings.HasPrefix(parsed.Path, folder) && !strings.Contains(parsed.Path, "..")
}

// compareVersions orders dotted versions ("1.10" after "1.9"), part by
// part: numerically when both parts are numbers, else as text.
func compareVersions(a, b string) int {
	aParts, bParts := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(aParts), len(bParts)); i++ {
		var x, y string
		if i < len(aParts) {
			x = aParts[i]
		}
		if i < len(bParts) {
			y = bParts[i]
		}
		xn, xErr := strconv.Atoi(x)
		yn, yErr := strconv.Atoi(y)
		switch {
		case xErr == nil && yErr == nil && xn != yn:
			if xn < yn {
				return -1
			}
			return 1
		case (xErr != nil || yErr != nil) && x != y:
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// releases holds the newest release the appcast lists, read again in the
// background. Until a read works, there is none.
type releases struct {
	feed   string
	base   *url.URL
	client *http.Client

	mu      sync.RWMutex
	current *Release
}

func newReleases(feed string, client *http.Client) (*releases, error) {
	base, err := url.Parse(feed)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("APPCAST_URL %q isn't a URL", feed)
	}
	return &releases{feed: feed, base: base, client: client}, nil
}

// Latest is the newest release read so far.
func (r *releases) Latest() (Release, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.current == nil {
		return Release{}, false
	}
	return *r.current, true
}

// Refresh reads the appcast and keeps its newest release. A failed read
// keeps the release from the last one that worked.
func (r *releases) Refresh(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.feed, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "espalier.abdeen.dev")
	response, err := r.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", r.feed, response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAppcastSize+1))
	if err != nil {
		return err
	}
	if len(body) > maxAppcastSize {
		return fmt.Errorf("%s is larger than %d bytes", r.feed, maxAppcastSize)
	}
	release, err := parseAppcast(strings.NewReader(string(body)), r.base)
	if err != nil {
		return err
	}
	r.mu.Lock()
	changed := r.current == nil || *r.current != release
	r.current = &release
	r.mu.Unlock()
	if changed {
		log.Printf("espalierd: release %s (%s), %s", release.Version, release.Build, release.URL)
	}
	return nil
}

// run reads the appcast now, then every interval, or every retryInterval
// while reads fail, until ctx ends.
func (r *releases) run(ctx context.Context, interval time.Duration) {
	for {
		wait := interval
		if err := r.Refresh(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("espalierd: reading the appcast: %v", err)
			wait = retryInterval
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}
