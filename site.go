package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"math"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// web holds the page's templates, the assets it links to (static, served at
// content-hashed names so browsers keep them), and the few files that live
// at fixed paths (root).
//
//go:embed web
var web embed.FS

// The site's own address, for links that must be absolute (the canonical
// link, the Open Graph image).
const siteURL = "https://espalier.abdeen.dev"

const (
	cacheForever = "public, max-age=31536000, immutable"
	cacheHour    = "public, max-age=3600"
	cacheRecheck = "no-cache"
)

// asset is a response body, compressed when that helps, with its type and
// entity tag.
type asset struct {
	body        []byte
	gzipped     []byte
	contentType string
	etag        string
}

func newAsset(body []byte, contentType string) *asset {
	sum := sha256.Sum256(body)
	a := &asset{body: body, contentType: contentType, etag: `W/"` + hex.EncodeToString(sum[:8]) + `"`}
	if compressible(contentType) {
		var buffer bytes.Buffer
		writer, _ := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
		_, _ = writer.Write(body)
		_ = writer.Close()
		if buffer.Len() < len(body) {
			a.gzipped = buffer.Bytes()
		}
	}
	return a
}

type site struct {
	releases *releases

	// static assets by URL path: each at its hashed path, the hashed path
	// by the plain one, and the files at fixed paths.
	static map[string]*asset
	hashed map[string]string
	root   map[string]*asset

	page     *template.Template
	notFound *asset

	mu        sync.Mutex
	pageKey   string
	pageAsset *asset
}

func newSite(releases *releases) (*site, error) {
	s := &site{
		releases: releases,
		static:   map[string]*asset{},
		hashed:   map[string]string{},
		root:     map[string]*asset{},
	}
	if err := s.loadFiles("web/static", "/static/", true); err != nil {
		return nil, err
	}
	if err := s.loadFiles("web/root", "/", false); err != nil {
		return nil, err
	}
	functions := template.FuncMap{
		"asset":        s.assetPath,
		"srcset":       s.srcset,
		"list":         func(items ...string) []string { return items },
		"themeCaption": func(theme string) string { return themes[theme].caption },
		"themeAlt":     func(theme string) string { return themes[theme].alt },
	}
	templates, err := template.New("").Funcs(functions).ParseFS(web, "web/templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("reading the templates: %w", err)
	}
	s.page = templates.Lookup("page.html")
	if s.page == nil {
		return nil, fmt.Errorf("web/templates has no page.html")
	}
	var notFound bytes.Buffer
	if err := templates.ExecuteTemplate(&notFound, "notfound.html", newPageData(Release{}, false)); err != nil {
		return nil, fmt.Errorf("rendering the not-found page: %w", err)
	}
	s.notFound = newAsset(notFound.Bytes(), "text/html; charset=utf-8")
	// Renders the page once now, so a template mistake stops the server
	// from starting rather than failing requests.
	if _, err := s.render(); err != nil {
		return nil, err
	}
	return s, nil
}

// loadFiles serves each file under dir at prefix plus its path, hashed
// into its name when hashed is set.
func (s *site) loadFiles(dir, prefix string, hashed bool) error {
	return fs.WalkDir(web, dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			return err
		}
		body, err := web.ReadFile(name)
		if err != nil {
			return err
		}
		contentType, ok := contentTypes[path.Ext(name)]
		if !ok {
			return fmt.Errorf("%s: no content type for %q", name, path.Ext(name))
		}
		a := newAsset(body, contentType)
		plain := prefix + strings.TrimPrefix(name, dir+"/")
		if !hashed {
			s.root[plain] = a
			return nil
		}
		sum := sha256.Sum256(body)
		ext := path.Ext(plain)
		withHash := strings.TrimSuffix(plain, ext) + "." + hex.EncodeToString(sum[:5]) + ext
		s.hashed[plain] = withHash
		s.static[withHash] = a
		return nil
	})
}

// assetPath is the templates' asset function: a static file's hashed path.
func (s *site) assetPath(plain string) (string, error) {
	if withHash, ok := s.hashed[plain]; ok {
		return withHash, nil
	}
	return "", fmt.Errorf("no static file %s", plain)
}

// srcset is the templates' srcset function: an image's sizes, from the
// files named images/<name>-<width>.webp.
func (s *site) srcset(name string, widths ...int) (template.Srcset, error) {
	candidates := make([]string, 0, len(widths))
	for _, width := range widths {
		file, err := s.assetPath(fmt.Sprintf("/static/images/%s-%d.webp", name, width))
		if err != nil {
			return "", err
		}
		candidates = append(candidates, fmt.Sprintf("%s %dw", file, width))
	}
	return template.Srcset(strings.Join(candidates, ", ")), nil
}

// pageData is what the page shows about the release.
type pageData struct {
	HasRelease  bool
	Version     string
	DownloadURL string
	Size        string
	Published   string
	MacOS       string
	SiteURL     string
	Year        int
}

func newPageData(release Release, ok bool) pageData {
	data := pageData{
		HasRelease:  ok,
		DownloadURL: "/download",
		MacOS:       "27",
		SiteURL:     siteURL,
		Year:        time.Now().Year(),
	}
	if !ok {
		return data
	}
	data.Version = release.Version
	data.DownloadURL = release.URL
	if release.Size > 0 {
		data.Size = fmt.Sprintf("%.0f MB", math.Max(1, math.Round(float64(release.Size)/1e6)))
	}
	if !release.Published.IsZero() {
		data.Published = release.Published.Format("January 2, 2006")
	}
	if major, _, _ := strings.Cut(release.MinimumSystemVersion, "."); major != "" {
		data.MacOS = major
	}
	return data
}

// render is the page for the latest release, rendered again only when the
// release changes.
func (s *site) render() (*asset, error) {
	release, ok := s.releases.Latest()
	key := fmt.Sprint(ok, release)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pageAsset != nil && s.pageKey == key {
		return s.pageAsset, nil
	}
	var page bytes.Buffer
	if err := s.page.Execute(&page, newPageData(release, ok)); err != nil {
		return nil, fmt.Errorf("rendering the page: %w", err)
	}
	s.pageKey, s.pageAsset = key, newAsset(page.Bytes(), "text/html; charset=utf-8")
	return s.pageAsset, nil
}

func (s *site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w.Header())
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case "/":
		page, err := s.render()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		serve(w, r, page, cacheRecheck, http.StatusOK)
	case "/download":
		s.serveDownload(w, r)
	case "/healthz":
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	default:
		if a, ok := s.static[r.URL.Path]; ok {
			serve(w, r, a, cacheForever, http.StatusOK)
		} else if a, ok := s.root[r.URL.Path]; ok {
			serve(w, r, a, cacheHour, http.StatusOK)
		} else {
			serve(w, r, s.notFound, cacheRecheck, http.StatusNotFound)
		}
	}
}

// serveDownload sends the browser to the latest DMG: a link that stays the
// same from release to release.
func (s *site) serveDownload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	release, ok := s.releases.Latest()
	if !ok {
		w.Header().Set("Retry-After", "60")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("The download isn't ready right now. Give it a minute and try again.\n"))
		return
	}
	http.Redirect(w, r, release.URL, http.StatusFound)
}

// serve writes a, compressed when the browser takes gzip, or Not Modified
// when the browser has it already.
func serve(w http.ResponseWriter, r *http.Request, a *asset, cacheControl string, status int) {
	header := w.Header()
	header.Set("Content-Type", a.contentType)
	header.Set("Cache-Control", cacheControl)
	header.Set("ETag", a.etag)
	if a.gzipped != nil {
		header.Add("Vary", "Accept-Encoding")
	}
	if status == http.StatusOK && etagMatches(r.Header.Get("If-None-Match"), a.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := a.body
	if a.gzipped != nil && acceptsGzip(r.Header.Get("Accept-Encoding")) {
		header.Set("Content-Encoding", "gzip")
		body = a.gzipped
	}
	header.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func etagMatches(ifNoneMatch, etag string) bool {
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

// acceptsGzip reports whether an Accept-Encoding header takes gzip.
func acceptsGzip(acceptEncoding string) bool {
	for _, part := range strings.Split(acceptEncoding, ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		coding = strings.ToLower(strings.TrimSpace(coding))
		if coding != "gzip" && coding != "*" {
			continue
		}
		if q, ok := strings.CutPrefix(strings.ReplaceAll(params, " ", ""), "q="); ok {
			if value, err := strconv.ParseFloat(q, 64); err == nil && value == 0 {
				continue
			}
		}
		return true
	}
	return false
}

// The page loads nothing but its own CSS and images, and runs no script.
func setSecurityHeaders(header http.Header) {
	header.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	header.Set("Strict-Transport-Security", "max-age=31536000")
	header.Set("Cross-Origin-Opener-Policy", "same-origin")
	header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), browsing-topics=()")
}

// themes describes the page's screenshot of each of Espalier's themes.
var themes = map[string]struct{ caption, alt string }{
	"classic": {
		caption: "Crisp and standard, using your Mac's system accent color.",
		alt:     "Espalier's History view in the Classic theme with a blue accent.",
	},
	"orchard": {
		caption: "Warm paper and stone tones instead of standard gray. Shown in Clay.",
		alt:     "Espalier's History view in the Orchard theme (Clay), with warm paper tones and a terracotta accent.",
	},
	"islands": {
		caption: "Floating card panes over a softer background. Shown in Lagoon.",
		alt:     "Espalier's History view in the Islands theme (Lagoon), with floating card panes and a teal accent.",
	},
	"pop": {
		caption: "High-contrast with a full-color sidebar. Shown in Magenta.",
		alt:     "Espalier's History view in the Pop theme (Magenta), with a pink-tinted sidebar and magenta accent.",
	},
}

var contentTypes = map[string]string{
	".css":  "text/css; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".ico":  "image/x-icon",
	".jpg":  "image/jpeg",
	".png":  "image/png",
	".svg":  "image/svg+xml",
	".txt":  "text/plain; charset=utf-8",
	".webp": "image/webp",
	".xml":  "application/xml; charset=utf-8",
}

func compressible(contentType string) bool {
	return strings.HasPrefix(contentType, "text/") ||
		strings.HasPrefix(contentType, "image/svg") ||
		strings.HasPrefix(contentType, "application/xml") ||
		contentType == "image/x-icon"
}
