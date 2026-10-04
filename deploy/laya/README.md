# The Laya sidecar

Cartograph asks a decision model typed questions about what people
write: whether a statement is written as the discipline asks, what a
sentence a person typed describes, and which existing record says the
same (docs/adr/0023). Laya is that model, open under Apache 2.0. It needs
the ONNX runtime, which Cartograph's static binary does not carry, so it
runs beside Cartograph, and Cartograph reaches it over HTTP.

It is off unless you turn it on. Without it, every check and answer
Cartograph had before is unchanged.

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

The sidecar is installed with the flake's Node, from the lockfile here,
into `~/.cache/cartograph/laya` (`CARTOGRAPH_LAYA_HOME`), never into the
repository. The first start downloads the model (about 1.7 GB) there and
takes a few minutes; later starts take seconds. onnxruntime-node's
install script is skipped: the package ships its CPU binaries, and the
script only fetches GPU ones.

Without the recipes, Node 20 or later:

    cd deploy/laya
    npm ci --ignore-scripts
    npm start

and start Cartograph with:

    CARTOGRAPH_DECIDE=laya CARTOGRAPH_DECIDE_URL=http://127.0.0.1:8411 cartograph serve <vault>

`GET /ready` answers 200 once the model is loaded.

With compose, `docker compose --profile decide up -d` starts it beside
Cartograph and keeps the model in a volume.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `LAYA_HOST` | `127.0.0.1` | Where it listens. Keep it off any network people reach: it has no authentication of its own. |
| `LAYA_PORT` | `8411` | The port. |
| `LAYA_CHECKPOINT` | Laya's English checkpoint | `multilingual` for plans in other languages. |
| `LAYA_CACHE` | Laya's own cache | Where the model is kept. |

It needs about 2 GB of memory and answers a call in roughly 30 to 150 ms
on a server CPU. Cartograph gives each call `CARTOGRAPH_DECIDE_TIMEOUT`
(5 s) and caches answers, so a slow or absent sidecar only means the
checks it would add are not shown.
