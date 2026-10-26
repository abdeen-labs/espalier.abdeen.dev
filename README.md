# espalier.abdeen.dev

The site for [Espalier](https://github.com/abdeen-labs/espalier), the Git
client for the Mac, at `https://espalier.abdeen.dev`.

`espalierd` is a single Go binary (standard library only) with the page, its
stylesheet, and its images built in. It reads Espalier's appcast,
`https://updates.abdeen.industries/espalier/appcast.xml`, when it starts and
every 10 minutes after, so the download button always names and links the
newest release. Publishing a release never redeploys the site.

| Path | |
|---|---|
| `/` | The page, rendered again when the release changes. `no-cache`, with an ETag. |
| `/download` | A `302` to the newest DMG: a link that stays the same from release to release. `503` until the first read of the appcast works. |
| `/static/…` | Assets at content-hashed names, cached for a year. Text is gzipped. |
| `/robots.txt`, `/sitemap.xml`, `/favicon.ico`, `/apple-touch-icon.png` | Fixed paths, cached for an hour. |
| `/healthz` | `200`, for the host's healthcheck. |

The page's one other host is `stats.abdeen.industries`, the self-hosted
Umami, and its Content-Security-Policy allows that host and nothing else.
Umami counts page views and Web Vitals (`data-performance`), on
`espalier.abdeen.dev` only (`data-domains`), so a local server counts
nothing. `stats.js` sends an event for each link marked `data-event`, each
theme picked, and each not-found page, with its `data-event-*` attributes as
the event's data. It follows the Mac's light or dark appearance, and the
theme switcher is radio buttons and CSS. A link to the DMG only ever goes to
the appcast's own folder on the update host, over HTTPS.

## Layout

```
main.go, release.go, site.go   The server: appcast reading, rendering, serving.
web/templates/                 page.html, footer.html, icons.html (an icon sprite), notfound.html, stats.html (Umami's script tags).
web/static/                    site.css, stats.js, images/ (made by scripts/images.sh), brand/ (Abdeen Labs lockups).
web/root/                      Files served at fixed paths.
scripts/images.sh              Makes the images from Espalier itself.
```

## Development

```sh
go run .                 # http://localhost:8080
go test ./...
```

Edits to `web/` show after a restart: the files are built into the binary.

## Images

The screenshots are Espalier's own windows, drawn by its Showcase captures
(`Packages/EspalierKit/Tests/EspalierCaptures/Showcase` in the Espalier
repository) on a demo project, in Classic, Frost, Islands, and Pop, light
and dark. The icon comes from Espalier's Icon Composer file. To make them
again after Espalier's look changes:

```sh
ESPALIER=~/Developer/Espalier scripts/images.sh
```

It needs Xcode (for Icon Composer's `ictool`), ImageMagick, and `cwebp`
(`brew install imagemagick webp`).

## Deploying

The `Dockerfile` builds a scratch image with the binary and the CA
certificates, about 13 MB. The server listens on `PORT` (8080 when it's
unset) and answers `/healthz` for a healthcheck; `APPCAST_URL` overrides the
feed, for testing. It needs nothing else. To check a deploy:

```sh
curl -sI https://espalier.abdeen.dev/
curl -sI https://espalier.abdeen.dev/download
```

The first answers `200`, the second `302` to the DMG on
`updates.abdeen.industries`.

## Licenses

The icons are from [Iconoir](https://iconoir.com), MIT License; see
`LICENSE-THIRD-PARTY`. The GitHub mark, a link to Espalier's repository, is
GitHub's, from [Primer Octicons](https://github.com/primer/octicons), used as
GitHub's logo guidelines allow. The Abdeen Labs lockups come from the Abdeen brand
repository.
