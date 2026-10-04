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

Node 20 or later:

    cd deploy/laya
    npm install
    npm start

npm 11 warns that onnxruntime-node's install script is not approved.
Leave it unapproved: the package ships its CPU binaries, and the script
only fetches GPU ones. The first start downloads the model (about 1.7 GB) into Laya's cache,
then answers on http://127.0.0.1:8411. `GET /ready` answers 200 once the
model is loaded. Then start Cartograph with:

    CARTOGRAPH_DECIDE=laya CARTOGRAPH_DECIDE_URL=http://127.0.0.1:8411 cartograph serve <vault>

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
