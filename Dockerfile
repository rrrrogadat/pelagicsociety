# ---- CSS build ----
FROM alpine:3.20 AS css
WORKDIR /app
RUN apk add --no-cache curl
ARG TW_VERSION=v3.4.13
ARG TARGETARCH
RUN case "$TARGETARCH" in \
      amd64) TW_ASSET=tailwindcss-linux-x64 ;; \
      arm64) TW_ASSET=tailwindcss-linux-arm64 ;; \
      *) echo "unsupported arch: $TARGETARCH" && exit 1 ;; \
    esac && \
    curl -sSL -o /usr/local/bin/tailwindcss \
      https://github.com/tailwindlabs/tailwindcss/releases/download/${TW_VERSION}/${TW_ASSET} && \
    chmod +x /usr/local/bin/tailwindcss
COPY tailwind.config.js ./
COPY web ./web
RUN tailwindcss -i ./web/static/css/input.css -o ./web/static/css/app.css --minify

# ---- Go build ----
FROM golang:1.22-alpine AS build
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
COPY --from=css /app/web/static/css/app.css ./web/static/css/app.css
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/pelagicsociety .

# ---- Runtime ----
FROM gcr.io/distroless/static-debian12
WORKDIR /
COPY --from=build /out/pelagicsociety /pelagicsociety
EXPOSE 8080
ENTRYPOINT ["/pelagicsociety"]
