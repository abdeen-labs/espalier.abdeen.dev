# syntax=docker/dockerfile:1

# ── build ───────────────────────────────────────────────────────────────────
FROM golang:1.27-alpine AS build

# The certificates the server needs to read the appcast over HTTPS.
RUN apk add --no-cache ca-certificates

WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux

# Standard library only, so there is nothing to download first.
COPY go.mod ./
COPY *.go ./
COPY web ./web
RUN go build -trimpath -ldflags="-s -w" -o /out/espalierd .

# ── runtime ─────────────────────────────────────────────────────────────────
# The binary, with the page and its images built in, and the certificates.
FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/espalierd /espalierd

USER 65532:65532
EXPOSE 8080

ENTRYPOINT ["/espalierd"]
