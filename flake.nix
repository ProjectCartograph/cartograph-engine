{
  description = "Cartograph engine: one pinned toolchain, the binary, the container image, on x86_64 and aarch64";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  # A Rust toolchain with the wasm32-wasip1 standard library, which
  # nixpkgs does not ship. It builds the CRDT module (crdt/) and nothing
  # else; the development shell does not carry it.
  inputs.rust-overlay.url = "github:oxalica/rust-overlay";
  inputs.rust-overlay.inputs.nixpkgs.follows = "nixpkgs";

  outputs = { self, nixpkgs, rust-overlay }:
    let
      # Both architectures the releases target, plus macOS for laptops.
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forEachSystem = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = builtins.replaceStrings [ "\n" ] [ "" ] (builtins.readFile ./VERSION);
      trim = f: builtins.replaceStrings [ "\n" " " ] [ "" "" ] (builtins.readFile f);
    in
    {
      packages = forEachSystem (pkgs:
        let
          lib = pkgs.lib;

          # The web build the binary embeds: the cartograph-ui release
          # UI_VERSION names, pinned by UI_SHA256. scripts/fetch-ui reads
          # the same two files, so `just` and `nix build` embed the same
          # bytes.
          ui = pkgs.fetchurl {
            url = "https://github.com/ProjectCartograph/cartograph-ui/releases/download/${trim ./UI_VERSION}/dist.tar.gz";
            sha256 = trim ./UI_SHA256;
          };

          # The Automerge module the engine embeds (docs/adr/0007): the
          # crate in crdt/, for wasm32-wasip1, with every dependency taken
          # from Cargo.lock and the build directory remapped out of the
          # output, so two builds give the same bytes. `just generate`
          # copies it to internal/crdt/automerge and `just drift` refuses
          # a committed module that differs.
          rustWasm = (rust-overlay.lib.mkRustBin { } pkgs).stable."1.98.1".minimal.override {
            targets = [ "wasm32-wasip1" ];
          };
          rustPlatformWasm = pkgs.makeRustPlatform { cargo = rustWasm; rustc = rustWasm; };
          automerge-wasm = pkgs.stdenv.mkDerivation {
            pname = "automerge-wasm";
            version = "0.1.0";
            src = lib.fileset.toSource {
              root = ./crdt;
              fileset = lib.fileset.unions [ ./crdt/Cargo.toml ./crdt/Cargo.lock ./crdt/src ];
            };
            cargoDeps = rustPlatformWasm.importCargoLock { lockFile = ./crdt/Cargo.lock; };
            nativeBuildInputs = [ rustWasm rustPlatformWasm.cargoSetupHook ];
            buildPhase = ''
              runHook preBuild
              top=$(realpath "$NIX_BUILD_TOP")
              export RUSTFLAGS="--remap-path-prefix=$top=/build --remap-path-prefix=$NIX_BUILD_TOP=/build --remap-path-prefix=$cargoDeps=/cargo"
              export SOURCE_DATE_EPOCH=0
              cargo build --offline --frozen --release --target wasm32-wasip1
              runHook postBuild
            '';
            installPhase = ''
              runHook preInstall
              install -Dm444 target/wasm32-wasip1/release/cartograph_crdt.wasm $out/automerge.wasm
              runHook postInstall
            '';
            dontFixup = true;
            meta.license = lib.licenses.asl20;
          };

          # The binary. Tests run in the build (buildGoModule's checkPhase),
          # so `nix build` is also the gate.
          cartograph = pkgs.buildGoModule {
            pname = "cartograph";
            inherit version;
            src = lib.cleanSource ./.;
            vendorHash = "sha256-wmRy0uULoA0ZKcmL9kPCIpzWEsFFLfrGb5xLAbAPSVU=";
            subPackages = [ "cmd/cartograph" ];
            env.CGO_ENABLED = 0;
            ldflags = [ "-s" "-w" "-X main.version=${version}" ];
            preBuild = ''
              rm -rf internal/spa/dist && mkdir -p internal/spa/dist
              tar -xzf ${ui} -C internal/spa/dist
            '';
            # subPackages narrows the check phase to cmd/cartograph, which
            # has no tests of its own; the gate is every package.
            checkPhase = ''
              runHook preCheck
              go test -count=1 ./...
              runHook postCheck
            '';
            meta = {
              description = "Cartograph: a vault of project, goal and KPI definitions, with its interface embedded";
              mainProgram = "cartograph";
              license = lib.licenses.asl20;
            };
          };

          # The Laya decision model (docs/adr/0023): its ONNX bundle at one
          # commit of its Hugging Face repository, each file a fixed-output
          # fetch, so no run downloads it and every run reads the same
          # weights. The sidecar reads it from the store (LAYA_MODEL_DIR).
          layaRev = "68f27dfe5a27a54fb2b1fefc432f43f972e90868";
          layaFile = file: hash: pkgs.fetchurl {
            url = "https://huggingface.co/receptron/laya-onnx/resolve/${layaRev}/${file}";
            inherit hash;
          };
          laya-model = pkgs.linkFarm "laya-model-${builtins.substring 0 7 layaRev}" [
            { name = "laya.onnx"; path = layaFile "laya.onnx" "sha256-qHTrJUtYsPyx561W+7GIwp1k4IyaRraJQz4fUsZtuh4="; }
            { name = "laya.onnx.data"; path = layaFile "laya.onnx.data" "sha256-SHdGNjqNpXvK20NFNSmX0ioPuQ1wqiLGhWZo0CMkKro="; }
            { name = "laya_config.json"; path = layaFile "laya_config.json" "sha256-UEkAXcauPKXoLMfYXEITV9XFQ4FzAMjoxSgd28abtWE="; }
            { name = "tokenizer/tokenizer.json"; path = layaFile "tokenizer/tokenizer.json" "sha256-bIqqmlQghPJFfqt3XU7rUfkqcMD9neKNXtsN3sPAjTA="; }
            { name = "tokenizer/tokenizer_config.json"; path = layaFile "tokenizer/tokenizer_config.json" "sha256-UARN5g2qpz35fSYuFaQNT68BYOfXQt9ks3eHehMg3RI="; }
          ];

          # The Laya sidecar (deploy/laya): its packages from the lockfile,
          # the ONNX runtime's CPU library patched to the store's, and the
          # model above. `nix run .#laya` serves it; nothing is fetched at
          # run time.
          laya = pkgs.buildNpmPackage {
            pname = "cartograph-laya";
            version = "0.1.2";
            src = ./deploy/laya;
            npmDepsHash = "sha256-+AOa10zXnELlogbEVbdiIoOoSjMSUOzMXSKY3ZZxIlE=";
            npmFlags = [ "--ignore-scripts" ];
            dontNpmBuild = true;
            nativeBuildInputs = [ pkgs.makeWrapper ] ++ lib.optionals pkgs.stdenv.hostPlatform.isLinux [ pkgs.autoPatchelfHook ];
            buildInputs = lib.optionals pkgs.stdenv.hostPlatform.isLinux [ pkgs.stdenv.cc.cc.lib ];
            installPhase = ''
              runHook preInstall
              mkdir -p $out/lib/laya $out/bin
              cp -r node_modules server.mjs package.json $out/lib/laya/
              # Only this platform's ONNX runtime is kept.
              ort=$out/lib/laya/node_modules/onnxruntime-node/bin
              for d in $ort/napi-v*/*; do
                case "$(basename "$d")" in
                  ${if pkgs.stdenv.hostPlatform.isLinux then "linux" else "darwin"}) ;;
                  *) rm -rf "$d" ;;
                esac
              done
              for d in $ort/napi-v*/*/*; do
                case "$(basename "$d")" in
                  ${if pkgs.stdenv.hostPlatform.isAarch64 then "arm64" else "x64"}) ;;
                  *) rm -rf "$d" ;;
                esac
              done
              makeWrapper ${pkgs.nodejs_22}/bin/node $out/bin/cartograph-laya \
                --add-flags $out/lib/laya/server.mjs \
                --set-default LAYA_MODEL_DIR ${laya-model}
              runHook postInstall
            '';
            meta = {
              description = "The Laya decision model sidecar Cartograph asks (docs/adr/0023)";
              mainProgram = "cartograph-laya";
              license = lib.licenses.asl20;
            };
          };

          # A container image is a Linux root filesystem: the binary, CA
          # certificates, a writable /vault owned by the runtime user, and
          # the binary's own readiness check. No shell, no browser.
          imageFor = { name, extra ? [ ], env ? [ ] }: pkgs.dockerTools.buildLayeredImage {
            inherit name;
            tag = version;
            contents = [ cartograph pkgs.cacert ] ++ extra;
            fakeRootCommands = ''
              mkdir -p ./vault ./tmp
              chown 65532:65532 ./vault
              chmod 1777 ./tmp
            '';
            config = {
              Entrypoint = [ "${cartograph}/bin/cartograph" ];
              Cmd = [ "serve" ];
              User = "65532:65532";
              Env = [ "CARTOGRAPH_VAULT=/vault" "CARTOGRAPH_LOG_FORMAT=json" "HOME=/tmp" ] ++ env;
              ExposedPorts = { "8080/tcp" = { }; };
              Volumes = { "/vault" = { }; };
              Healthcheck = {
                Test = [ "CMD" "${cartograph}/bin/cartograph" "ready" ];
                Interval = 10000000000;
                Timeout = 3000000000;
                StartPeriod = 5000000000;
              };
            };
          };
        in
        { inherit cartograph automerge-wasm laya laya-model; default = cartograph; }
        // lib.optionalAttrs pkgs.stdenv.hostPlatform.isLinux {
          image = imageFor { name = "cartograph"; };
          # The same, plus Chromium for PDF printing.
          image-chromium = imageFor {
            name = "cartograph-chromium";
            extra = [ pkgs.chromium ];
            env = [ "CARTOGRAPH_CHROMIUM=${pkgs.chromium}/bin/chromium" ];
          };
        });

      devShells = forEachSystem (pkgs:
        let haveChromium = pkgs.stdenv.hostPlatform.isLinux; in {
          default = pkgs.mkShell {
            name = "cartograph-engine";
            packages = with pkgs; [
              go
              go-tools # staticcheck
              gopls # the language server: just check runs its diagnostics
              (python3.withPackages (ps: [ ps.pyyaml ])) # scripts/check-compat
              just
              bashInteractive
              coreutils
              diffutils
              gnugrep
              gnused
              curl
              git
              jujutsu # version control: colocated with git, the one agents use
              postgresql # `just test-postgres` starts a throwaway server
              # The Laya sidecar (deploy/laya): `just serve laya`
              # starts it beside the server (docs/adr/0023).
              nodejs_22
              # The Helm chart in deploy/helm: `just helm-lint` renders and
              # validates it, `just helm-kind` installs it on a kind cluster
              # (Docker comes from the host).
              kubernetes-helm
              kubeconform
              kind
              kubectl
            ] ++ pkgs.lib.optionals haveChromium [ chromium ];
            shellHook = ''
              ${pkgs.lib.optionalString haveChromium ''export CHROMIUM="${pkgs.chromium}/bin/chromium"''}
              export GOTOOLCHAIN=local
              export CARTOGRAPH_TOOLCHAIN=1
            '';
          };
        });

      apps = forEachSystem (pkgs:
        let cartograph = self.packages.${pkgs.stdenv.hostPlatform.system}.cartograph; in {
          default = { type = "app"; program = "${cartograph}/bin/cartograph"; meta.description = "Run the cartograph command line"; };
          cartograph = self.apps.${pkgs.stdenv.hostPlatform.system}.default;
        });

      checks = forEachSystem (pkgs: { cartograph = self.packages.${pkgs.stdenv.hostPlatform.system}.cartograph; });
      formatter = forEachSystem (pkgs: pkgs.nixpkgs-fmt);
    };
}
