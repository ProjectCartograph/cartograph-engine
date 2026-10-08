# The Laya sidecar

Cartograph asks a decision model typed questions about what people
write: whether a statement is written as the discipline asks, what a
sentence a person typed describes, and which existing record says the
same (docs/adr/0023). Laya is that model, open under Apache 2.0. It needs
the ONNX runtime, which Cartograph's static binary does not carry, so it
runs beside Cartograph, and Cartograph reaches it over HTTP.

Cartograph asks it by default (docs/adr/0030); `CARTOGRAPH_DECIDE=off`
turns it off. Without it, the checks that judge meaning are left out,
`GET /decision-model` lists them, and nothing else changes.

## Run it

From the repository, through the flake like every recipe:

    just serve laya

`laya` starts the sidecar, waits until the model is loaded, then serves
a copy of the example on 127.0.0.1:8080 with `CARTOGRAPH_DECIDE=laya`;
stopping the server stops the sidecar. The arguments come in any order:
an address to listen elsewhere (`just serve laya 0.0.0.0:8080`), and
`laya_port=N` to move the sidecar from 8411. `just serve-vault <dir>
laya` does the same for your own vault, and `just laya` runs the
sidecar on its own.

The sidecar is the flake's `laya` package: its packages built from the
lockfile here, the ONNX runtime's CPU library patched to the store's,
and the model pinned to one commit of its Hugging Face repository, each
file a fixed-output fetch. `nix run .#laya` serves it; nothing is
installed or downloaded when it starts, and no cache outside the Nix
store is read. The first build fetches the model (about 1.7 GB) into the
store, once.

Start Cartograph beside it with:

    CARTOGRAPH_DECIDE=laya CARTOGRAPH_DECIDE_URL=http://127.0.0.1:8411 cartograph serve <vault>

`GET /ready` answers 200 once the model is loaded.

## Platforms

The package and its image are built for x86_64 and arm64 Linux on one
x86_64 machine (`laya-linux-<arch>`, `image-laya-linux-<arch>`;
docs/CROSS.md), each with the ONNX runtime's library for that platform.
A release publishes one tag for both:
`ghcr.io/projectcartograph/cartograph-laya:<version>` (set
`CARTOGRAPH_LAYA_IMAGE` to it for compose). Before the tag is made, a
runner of each architecture runs the image and `just decide-measure
<url>` against it, which holds every answer to the one recorded on
x86_64 (`internal/engine/testdata/decide/answers.json`) within 0.005:
the same model answers the same on both.

With compose, `docker compose --profile decide up -d` starts it beside
Cartograph and keeps the model in a volume.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `LAYA_HOST` | `127.0.0.1` | Where it listens. Keep it off any network people reach: it has no authentication of its own. |
| `LAYA_PORT` | `8411` | The port. |
| `LAYA_MODEL_DIR` | the pinned model in the store | The model bundle read as it is. The flake's package sets it; nothing is downloaded. |

It needs about 2 GB of memory and answers a call in roughly 30 to 150 ms
on a server CPU. Cartograph gives each call `CARTOGRAPH_DECIDE_TIMEOUT`
(5 s) and caches answers, so a slow or absent sidecar only means the
checks it would add are not shown.
