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

# The gate, after every edit, and what CI runs: unit tests with fake
# adapters, a few seconds. The real adapters' tests are test-integration.
test: embed
    #!{{toolchain}} bash
    set -euo pipefail
    # Compile every test binary first: the budget is for the tests, and a
    # cold CI runner spends most of ten seconds compiling.
    go test -count=1 -run '^$' ./... >/dev/null
    # The stores' tests write SQLite files and vaults to the temporary
    # directory; on a runner's disk that alone is most of the budget. A
    # tmpfs where there is one (Linux's /dev/shm), the default elsewhere.
    # Go builds the test binaries in GOTMPDIR, kept where it was: a tmpfs
    # may be small, or not allow running what is on it.
    if [ -d /dev/shm ] && [ -w /dev/shm ]; then
      export GOTMPDIR="${GOTMPDIR:-${TMPDIR:-/tmp}}"
      TMPDIR=$(mktemp -d /dev/shm/cartograph-test.XXXXXX)
      export TMPDIR
      trap 'rm -rf "$TMPDIR"' EXIT
    fi
    start=$(date +%s%3N)
    go test -count=1 ./...
    end=$(date +%s%3N)
    echo "tests completed in $((end - start))ms"
    test $((end - start)) -lt 5000

# The real adapters, locally and never in CI: SQLite, the vault, the
# WebAssembly CRDT, the sync server's sockets, the serve stack, Postgres
# against a throwaway server. Nix makes them the same on every machine,
# so passing here is passing anywhere (scripts/dev just test-integration).
test-integration: embed
    #!{{toolchain}} bash
    set -euo pipefail
    dir=$(mktemp -d /tmp/cartograph-pg.XXXXXX)
    trap 'pg_ctl -D "$dir/data" -m immediate stop >/dev/null 2>&1 || true; rm -rf "$dir"' EXIT
    initdb -D "$dir/data" -U cartograph -A trust -E UTF8 --no-sync >/dev/null
    pg_ctl -D "$dir/data" -l "$dir/server.log" -w \
      -o "-k $dir -c listen_addresses= -c fsync=off -c synchronous_commit=off -c full_page_writes=off" \
      start >/dev/null || { cat "$dir/server.log"; exit 1; }
    export CARTOGRAPH_TEST_POSTGRES="postgres://cartograph@/postgres?host=$dir"
    go test -count=1 -tags integration ./...

# The whole suite with the Postgres adapters' tests, against a throwaway
# server from the flake: a fresh cluster in a temporary directory,
# reached over a UNIX socket with trust authentication, stopped and
# removed however the tests end. No Docker, no port.
test-postgres *args="./...": embed
    #!{{toolchain}} bash
    set -euo pipefail
    dir=$(mktemp -d /tmp/cartograph-pg.XXXXXX)
    trap 'pg_ctl -D "$dir/data" -m immediate stop >/dev/null 2>&1 || true; rm -rf "$dir"' EXIT
    initdb -D "$dir/data" -U cartograph -A trust -E UTF8 --no-sync >/dev/null
    pg_ctl -D "$dir/data" -l "$dir/server.log" -w \
      -o "-k $dir -c listen_addresses= -c fsync=off -c synchronous_commit=off -c full_page_writes=off" \
      start >/dev/null || { cat "$dir/server.log"; exit 1; }
    export CARTOGRAPH_TEST_POSTGRES="postgres://cartograph@/postgres?host=$dir"
    go test -count=1 -tags integration {{args}}

# What CI runs, in this order; green here is green there
ci: generate drift vet fmt-check lint arch test words clean-tree compat build

# --- the contract ---------------------------------------------------------

# Regenerate every committed, generated file from the contract
generate:
    rm -rf internal/contract/schemas internal/contract/flows internal/contract/guidance
    mkdir -p internal/contract/schemas internal/contract/flows internal/contract/guidance
    cp contract/schemas/*.json internal/contract/schemas/
    cp contract/flows/*.json internal/contract/flows/
    cp -r contract/guidance/. internal/contract/guidance/
    cd internal/api && go generate ./...
    # The Automerge module, built from crdt/ by the flake (docs/adr/0007).
    # Its bytes are those x86_64 Linux builds: a compiler hosted on another
    # architecture emits different, equivalent WebAssembly, so elsewhere
    # the committed module is kept and only x86_64 checks it for drift.
    if [ "$(uname -sm)" = "Linux x86_64" ]; then \
      install -m 644 "$(nix --extra-experimental-features 'nix-command flakes' build .#automerge-wasm --no-link --print-out-paths)/automerge.wasm" internal/crdt/automerge/automerge.wasm; \
    else \
      echo "automerge.wasm: built and checked on x86_64 Linux only; kept as committed"; \
    fi

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

# staticcheck, the analyser the style guide names, and gopls, the
# language server's own diagnostics: either finding anything fails
lint: embed
    staticcheck ./...
    out="$(gopls check $(git ls-files '*.go' | grep -v '/gen/'))"; [ -z "$out" ] || { echo "$out"; exit 1; }
    actionlint .github/workflows/*.yml

# The dependency rule: inward only (internal/arch)
arch: embed
    go test -count=1 -tags arch ./internal/arch/

# No organisation's words, no em dashes in what a person reads
words:
    #!{{toolchain}} bash
    set -uo pipefail
    n=$(grep -rniE '\b(ministry|school|schools|pupil|cabinet|circular|vote|district|teacher|ecce)\b' --include='*.go' --include='*.json' --include='*.jsonl' --include='*.yaml' --include='*.yml' --include='*.md' . | grep -v '/dist/' | grep -vE '^./docs/(TAXONOMY|DESIGN_RULES).md' | wc -l)
    d=$(grep -rnE '—' examples/ contract/guidance/ contract/flows/ 2>/dev/null | wc -l)
    echo "domain words: $n, em dashes in the example: $d"
    test "$n" -eq 0 && test "$d" -eq 0

# Nothing that is not the product is tracked (scripts/check-clean-tree)
clean-tree:
    scripts/check-clean-tree

# Commit messages since main follow CONTRIBUTING.md (scripts/check-commit-msg)
commit-check base="origin/main":
    scripts/check-commit-msg {{base}}

# No breaking change to the contract, the schemas or pkg/ since the last
# release without a major bump (VERSIONING.md)
compat base="": embed
    #!{{toolchain}} bash
    set -euo pipefail
    scripts/check-compat {{base}}
    # gorelease is not packaged in nixpkgs; the Go toolchain fetches it.
    # The base is the last release before this commit: a tag on HEAD is
    # the release being checked.
    tag=$(git tag --list 'v*' --sort=-v:refname --no-contains HEAD | head -n 1)
    if [ -z "$tag" ]; then exit 0; fi
    major=$(cut -d. -f1 VERSION)
    base=${tag#v}
    if [ "${base%%.*}" != "$major" ]; then
      # A new major has a new module path (/v<major>), so there is no base
      # for gorelease to compare with; check-compat above has already
      # required the bump.
      echo "VERSION $(cat VERSION) is a new major after $tag: no gorelease base"
      exit 0
    fi
    go run golang.org/x/exp/cmd/gorelease@latest -base="$tag" 2>&1 | tail -n 20

# --- build and run --------------------------------------------------------

build: embed
    go build ./...

# The binary, for this machine, into bin/
bin: embed
    mkdir -p bin && go build -trimpath -ldflags="-s -w -X main.version={{version}}" -o bin/cartograph ./cmd/cartograph

# Release binaries for every platform, cross-compiled here (pure Go, static)
release:
    #!{{toolchain}} bash
    set -euo pipefail
    # Every platform's binary, cross-compiled here by the flake
    # (docs/CROSS.md), with SHA256SUMS.
    rm -rf dist && mkdir -p dist
    cp "$(nix build .#release --no-link --print-out-paths)"/* dist/
    chmod u+w dist/*
    ls dist

# The container image, from the flake, for this machine's architecture;
# loads into docker as cartograph:<version> and cartograph:local, the tag
# compose.yaml and compose.ha.yaml run, so they never name a stale version
image:
    #!{{toolchain}} bash
    set -euo pipefail
    nix build .#image --out-link result-image
    name=$(docker load < result-image | sed -n 's/^Loaded image: //p')
    docker tag "$name" cartograph:local
    echo "tagged $name as cartograph:local"

# The Laya sidecar's image, its model inside, tagged cartograph-laya:local
# (compose.yaml's decide profile)
image-laya:
    #!{{toolchain}} bash
    set -euo pipefail
    nix build .#image-laya --out-link result-image-laya
    name=$(docker load < result-image-laya | sed -n 's/^Loaded image: //p')
    docker tag "$name" cartograph-laya:local
    echo "tagged $name as cartograph-laya:local"

# The highly available topology on this machine: Postgres, a one-shot
# import of the example, two replicas, nginx on :8080 (compose.ha.yaml).
# Needs `just image` first.
ha-up:
    docker compose -f compose.ha.yaml up -d --wait

# Stop the rehearsal and drop its database
ha-down:
    docker compose -f compose.ha.yaml down -v

# --- deployment -----------------------------------------------------------

# The Helm chart, without a cluster: its version is VERSION, it lints, it
# refuses values that would deploy something broken, and what it renders
# validates against the Kubernetes and CRD schemas (kubeconform fetches
# them)
helm-lint:
    #!{{toolchain}} bash
    set -euo pipefail
    chart=deploy/helm/cartograph
    v=$(cat VERSION)
    cv=$(sed -n 's/^version: *//p' $chart/Chart.yaml)
    av=$(sed -n 's/^appVersion: *"\{0,1\}\([^"]*\)"\{0,1\}/\1/p' $chart/Chart.yaml)
    if [ "$cv" != "$v" ] || [ "$av" != "$v" ]; then
      echo "$chart/Chart.yaml: version $cv, appVersion $av; VERSION is $v. Set both to $v."
      exit 1
    fi
    echo "chart version and appVersion: $v"
    helm lint --strict $chart
    for f in $chart/ci/*-values.yaml; do helm lint --strict --quiet $chart -f "$f"; done
    # Values the chart must refuse, each with the message an operator reads.
    refuse() {
      want=$1; shift
      if out=$(helm template t $chart "$@" 2>&1); then
        echo "rendered, but should refuse: $*"; exit 1
      fi
      grep -q -- "$want" <<<"$out" || { echo "refused without saying '$want': $*"; echo "$out"; exit 1; }
      echo "refuses: ${*:-default values} ($want)"
    }
    refuse "needs a Postgres store"
    refuse "needs a Postgres store" --set autoscaling.enabled=true --set replicaCount=1 --set vault.enabled=true
    refuse "choose one autoscaler" --set store.url=postgres://h/db --set autoscaling.enabled=true --set keda.enabled=true --set keda.prometheus.serverAddress=http://p:9090
    refuse "needs keda.prometheus.serverAddress" --set store.url=postgres://h/db --set keda.enabled=true
    refuse "must be greater than" --set store.url=postgres://h/db --set terminationGracePeriodSeconds=10
    refuse "needs an authenticator" --set store.url=postgres://h/db --set authz.mode=roles
    refuse "needs an authenticator" --set store.url=postgres://h/db --set authz.mode=access
    refuse "not both" --set store.url=postgres://h/db --set auth.mode=proxy --set authz.mode=access --set authz.access.existingConfigMap=x --set authz.access.mapping.roles.reader[0]=all
    refuse "values don't meet the specifications" --set store.url=mysql://h/db
    schemas=(-schema-location default
      -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{{{.Group}}/{{{{.ResourceKind}}_{{{{.ResourceAPIVersion}}.json')
    for f in $chart/ci/*-values.yaml; do
      echo "kubeconform: $f"
      helm template cartograph $chart -f "$f" | kubeconform -strict -summary "${schemas[@]}"
    done

# Install the chart on a throwaway kind cluster: the image the flake
# builds, two replicas on an in-cluster Postgres, then probe it and kill a
# pod (scripts/helm-kind). Needs Docker.
helm-kind:
    scripts/helm-kind

# Serve a copy of the example on 127.0.0.1:8080. Arguments in any order:
# an address, laya to run the Laya sidecar beside it (stopped with the
# server), laya_port=N to move the sidecar: `just serve laya`,
# `just serve 0.0.0.0:8080 laya` (scripts/serve)
serve *args: embed
    rm -rf /tmp/cartograph-example && cp -r examples/minimal /tmp/cartograph-example
    scripts/serve /tmp/cartograph-example {{args}}

# Serve any vault directory, with the arguments serve takes
serve-vault dir *args: embed
    scripts/serve "{{dir}}" {{args}}

# Measure every System-1 judgement against its examples whose answer is
# known, on the model the flake pins, and hold each to the score it was
# kept at (docs/adr/0030), and every answer to the one recorded on
# x86_64. Locally and at a release, never in CI: it runs the real model.
decide-measure url="":
    #!{{toolchain}} bash
    set -euo pipefail
    # A sidecar already running (an image a release is about to publish)
    # is measured where it is.
    if [ -n "{{url}}" ]; then
      CARTOGRAPH_DECIDE_URL="{{url}}" go test -count=1 -tags decide -run TestMeasureJudgements -v ./internal/engine/ | grep -E "^\s+measure_test|^(--- |ok|FAIL)"
      exit
    fi
    # Built first: the model is fetched into the store once, however long
    # that takes; starting it then takes seconds.
    laya=$(nix build .#laya --no-link --print-out-paths)
    port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')
    LAYA_PORT=$port "$laya/bin/cartograph-laya" >/tmp/cartograph-laya-$port.log 2>&1 &
    pid=$!
    trap 'kill $pid 2>/dev/null || true' EXIT
    ready=
    for _ in $(seq 1 120); do
      curl -sf "http://127.0.0.1:$port/ready" >/dev/null && { ready=1; break; }
      sleep 1
    done
    [ -n "$ready" ] || { echo "the model did not start:"; tail -5 /tmp/cartograph-laya-$port.log; exit 1; }
    CARTOGRAPH_DECIDE_URL="http://127.0.0.1:$port" go test -count=1 -tags decide -run TestMeasureJudgements -v ./internal/engine/ | grep -E "^\s+measure_test|^(--- |ok|FAIL)"

# Run the Laya sidecar on its own (deploy/laya, docs/adr/0023)
laya port="8411":
    LAYA_PORT={{port}} scripts/laya

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
    rm -rf bin dist result result-image result-image-laya internal/spa/dist internal/spa/dist.stamp

# --- judging a change agents use (docs/EVALUATING.md) ---------------------
# An evaluation directory lives outside the repository, beside its
# criteria file. Every run serves the frozen build from the Nix store, so
# nothing changing in the working tree touches it.

# Freeze the build under test: the flake built at a revision (@-, the last described change, by default)
eval-build dir rev="":
    #!{{toolchain}} bash
    set -euo pipefail
    # Any revision jj names (a change or commit id, short or whole, @-, a
    # bookmark), resolved to the whole commit id nix builds from.
    rev="$(jj log -r "{{ if rev == "" { "@-" } else { rev } }}" --no-graph -T commit_id)"
    mkdir -p "{{dir}}"
    out="$(nix build "git+file://{{justfile_directory()}}?rev=$rev#cartograph" --print-out-paths --out-link "{{dir}}/build")"
    "$out/bin/cartograph" eval build "{{dir}}" -commit "$rev" -store "$out"

# Serve a fresh traced run of the frozen build, and print the agent's prompt
eval-serve dir document agent="Agent":
    "$(readlink -f "{{dir}}/build")/bin/cartograph" eval serve "{{dir}}" -document "{{document}}" -agent "{{agent}}" -flake "{{justfile_directory()}}"

# Score a run from the server and its trace against the criteria
eval-score dir run criteria change_set:
    "$(readlink -f "{{dir}}/build")/bin/cartograph" eval score "{{dir}}" "{{run}}" -criteria "{{criteria}}" -change-set "{{change_set}}"

# Every run, its score and trace figures, and the streak against the bar
eval-status dir criteria="":
    "$(readlink -f "{{dir}}/build")/bin/cartograph" eval status "{{dir}}" {{ if criteria != "" { "-criteria " + quote(criteria) } else { "" } }}

# Stop a run's server, or every run's
eval-stop dir run="":
    "$(readlink -f "{{dir}}/build")/bin/cartograph" eval stop "{{dir}}" {{run}}
