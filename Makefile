.PHONY: dev css css-watch build run clean tailwind release

TAILWIND_VERSION ?= v3.4.13
TAILWIND_BIN     ?= ./bin/tailwindcss

UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)
ifeq ($(UNAME_S),Darwin)
  ifeq ($(UNAME_M),arm64)
    TAILWIND_ASSET := tailwindcss-macos-arm64
  else
    TAILWIND_ASSET := tailwindcss-macos-x64
  endif
else
  ifeq ($(UNAME_M),aarch64)
    TAILWIND_ASSET := tailwindcss-linux-arm64
  else
    TAILWIND_ASSET := tailwindcss-linux-x64
  endif
endif

tailwind:
	@mkdir -p bin
	@if [ ! -x $(TAILWIND_BIN) ]; then \
	  echo "downloading tailwindcss $(TAILWIND_VERSION)..."; \
	  curl -sSL -o $(TAILWIND_BIN) https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/$(TAILWIND_ASSET); \
	  chmod +x $(TAILWIND_BIN); \
	fi

css: tailwind
	$(TAILWIND_BIN) -i ./web/static/css/input.css -o ./web/static/css/app.css --minify

css-watch: tailwind
	$(TAILWIND_BIN) -i ./web/static/css/input.css -o ./web/static/css/app.css --watch

build: css
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/pelagicsociety .

# Cross-compile a self-contained linux/amd64 binary with CSS embedded.
# Used by infra/deploy/deploy.sh.
release: css
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/pelagicsociety .

run: build
	./bin/pelagicsociety

dev: css
	go run .

clean:
	rm -rf bin web/static/css/app.css
