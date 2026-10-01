# Cartograph engine. `just` lists recipes. Every recipe runs inside the
# toolchain flake.nix pins (scripts/toolchain enters `nix develop` unless
# the caller already did), so a laptop, CI and a release build the same
# way on either architecture.
set shell := ["scripts/toolchain", "bash", "-euo", "pipefail", "-c"]

toolchain := source_directory() / "scripts/toolchain"
version := `cat VERSION 2>/dev/null || echo 0.1.0`

default:
    #!/usr/bin/env bash
    just --list --unsorted

# --- the gate -------------------------------------------------------------

# The gate, after every edit: under ten seconds
test: embed
    #!{{toolchain}} bash
    set -euo pipefail
    start=$(date +%s%3N)
    go test -count=1 ./...
    end=$(date +%s%3N)
    echo "tests completed in $((end - start))ms"
    test $((end - start)) -lt 10000

# What CI runs, in this order; green here is green there
ci: generate drift vet fmt-check lint arch test words clean-tree compat build

# --- the contract ---------------------------------------------------------

# Regenerate every committed, generated file from the contract
generate:
    rm -rf internal/contract/schemas internal/contract/flows
    mkdir -p internal/contract/schemas internal/contract/flows
    cp contract/schemas/*.json internal/contract/schemas/
    cp contract/flows/*.json internal/contract/flows/
    cd internal/api && go generate ./...

# Fail if generate produced anything that is not committed
drift:
    git diff --exit-code -- .

# The web build the binary embeds, into internal/spa/dist: the
# cartograph-ui release UI_VERSION names, checked against UI_SHA256; or,
# given a path, a local build (a dist directory or dist.tar.gz)
ui source="":
    scripts/fetch-ui {{source}}

# What every recipe that compiles the binary needs: the pinned web build,
# or a local one `just ui <path>` already put in place
[private]
embed:
    scripts/fetch-ui --keep-local

# --- checks ---------------------------------------------------------------

vet: embed
    go vet ./...

# gofmt, as the style guide requires; `just fmt` rewrites
fmt-check:
    #!{{toolchain}} bash
    set -euo pipefail
    files=$(gofmt -l .)
    if [ -n "$files" ]; then echo "$files"; echo "run: just fmt"; exit 1; fi

fmt:
    gofmt -w .

# staticcheck, the analyser the style guide names
lint: embed
    staticcheck ./...

# The dependency rule: inward only (internal/arch)
arch: embed
    go test -count=1 ./internal/arch/

# No organisation's words, no em dashes in what a person reads
words:
    #!{{toolchain}} bash
    set -uo pipefail
    n=$(grep -rniE '\b(ministry|school|schools|pupil|cabinet|circular|vote|district|teacher|ecce)\b' --include='*.go' --include='*.json' --include='*.yaml' --include='*.yml' --include='*.md' . | grep -v '/dist/' | grep -vE '^./docs/(TAXONOMY|DESIGN_RULES).md' | wc -l)
    d=$(grep -rnE '—' examples/ 2>/dev/null | wc -l)
    echo "domain words: $n, em dashes in the example: $d"
    test "$n" -eq 0 && test "$d" -eq 0

# Nothing that is not the product is tracked (scripts/check-clean-tree)
clean-tree:
    scripts/check-clean-tree

# Commit messages since main follow STYLE.md (scripts/check-commit-msg)
commit-check base="origin/main":
    scripts/check-commit-msg {{base}}

# No breaking change to the contract, the schemas or pkg/ since the last
# release without a major bump (VERSIONING.md)
compat base="": embed
    #!{{toolchain}} bash
    set -euo pipefail
    scripts/check-compat {{base}}
    # gorelease is not packaged in nixpkgs; the Go toolchain fetches it.
    tag=$(git tag --list 'v*' --sort=-v:refname | head -n 1)
    if [ -n "$tag" ]; then go run golang.org/x/exp/cmd/gorelease@latest -base="$tag" 2>&1 | tail -n 20; fi

# --- build and run --------------------------------------------------------

build: embed
    go build ./...

# The binary, for this machine, into bin/
bin: embed
    mkdir -p bin && go build -trimpath -ldflags="-s -w -X main.version={{version}}" -o bin/cartograph ./cmd/cartograph

# Release binaries for both architectures (pure Go, static)
release: embed
    #!{{toolchain}} bash
    set -euo pipefail
    mkdir -p dist
    for arch in amd64 arm64; do
      CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags="-s -w -X main.version={{version}}" -o dist/cartograph-linux-$arch ./cmd/cartograph
      echo "dist/cartograph-linux-$arch"
    done

# The container image, from the flake, for this machine's architecture; loads into docker
image:
    #!{{toolchain}} bash
    set -euo pipefail
    nix build .#image --out-link result-image
    docker load < result-image

# Serve a copy of the example on 127.0.0.1:8080
serve addr="127.0.0.1:8080": embed
    rm -rf /tmp/cartograph-example && cp -r examples/minimal /tmp/cartograph-example
    go run ./cmd/cartograph serve /tmp/cartograph-example -addr {{addr}}

# Serve any vault directory
serve-vault dir addr="127.0.0.1:8080": embed
    go run ./cmd/cartograph serve "{{dir}}" -addr {{addr}}

# Validate a directory of manifests
validate dir="examples/minimal": embed
    go run ./cmd/cartograph validate "{{dir}}"

# Export, re-open the export, export again, diff: must be identical
roundtrip dir="examples/minimal": embed
    #!{{toolchain}} bash
    set -euo pipefail
    rm -rf /tmp/cartograph-rt && mkdir -p /tmp/cartograph-rt && cp -r "{{dir}}" /tmp/cartograph-rt/v1
    go run ./cmd/cartograph export /tmp/cartograph-rt/e1 /tmp/cartograph-rt/v1 >/dev/null
    cp -r /tmp/cartograph-rt/e1 /tmp/cartograph-rt/v2
    go run ./cmd/cartograph export /tmp/cartograph-rt/e2 /tmp/cartograph-rt/v2 >/dev/null
    diff -r /tmp/cartograph-rt/e1 /tmp/cartograph-rt/e2 && echo "round trip identical"

clean:
    rm -rf bin dist result result-image internal/spa/dist internal/spa/dist.stamp
