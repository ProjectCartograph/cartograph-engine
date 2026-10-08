// The Laya sidecar: Cartograph's decide/laya adapter asks it typed
// questions about a text over HTTP (docs/adr/0023). Laya needs the ONNX
// runtime, which Cartograph's static binary does not carry, so it runs
// beside it.
//
//   POST /decide  {"state": "...", "questions": {...}}  -> {"answers": {...}}
//                 the questions as Laya's systemOne takes them
//   GET  /ready   200 once the model is loaded, 503 before
//
// LAYA_HOST (127.0.0.1) and LAYA_PORT (8411) say where it listens;
// LAYA_CHECKPOINT picks a checkpoint (multilingual, for one), left out
// for Laya's default English one; LAYA_CACHE is where the model is kept,
// unless LAYA_MODEL_DIR names it (the flake's package does).
import { createServer } from "node:http";
import { Laya } from "@receptron/laya";

const host = process.env.LAYA_HOST ?? "127.0.0.1";
const port = Number(process.env.LAYA_PORT ?? 8411);
const maxBody = 64 * 1024;

let laya;
// LAYA_MODEL_DIR, set by the flake's package, is the model in the Nix
// store: read as it is, never downloaded.
const loading = Laya.load(
  process.env.LAYA_MODEL_DIR
    ? { modelDir: process.env.LAYA_MODEL_DIR }
    : {
        ...(process.env.LAYA_CHECKPOINT ? { subfolder: process.env.LAYA_CHECKPOINT } : {}),
        ...(process.env.LAYA_CACHE ? { cacheDir: process.env.LAYA_CACHE } : {}),
      },
).then((model) => {
  laya = model;
  console.log(JSON.stringify({ msg: "laya ready", host, port }));
});
loading.catch((err) => {
  console.error(JSON.stringify({ msg: "laya failed to load", error: String(err) }));
  process.exit(1);
});

function reply(res, status, body) {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
}

createServer((req, res) => {
  if (req.method === "GET" && req.url === "/ready") {
    return reply(res, laya ? 200 : 503, { ready: !!laya });
  }
  if (req.method !== "POST" || req.url !== "/decide") {
    return reply(res, 404, { error: "not found" });
  }
  if (!laya) {
    return reply(res, 503, { error: "the model is still loading" });
  }
  let size = 0;
  const chunks = [];
  req.on("data", (c) => {
    size += c.length;
    if (size > maxBody) {
      reply(res, 413, { error: "too large" });
      req.destroy();
      return;
    }
    chunks.push(c);
  });
  req.on("end", async () => {
    if (res.writableEnded) return;
    try {
      const { state, questions } = JSON.parse(Buffer.concat(chunks).toString("utf8"));
      if (typeof state !== "string" || !questions || typeof questions !== "object") {
        return reply(res, 400, { error: "want a state and questions" });
      }
      const result = await laya.systemOne(state, questions);
      reply(res, 200, { answers: result.answers });
    } catch (err) {
      reply(res, 500, { error: String(err) });
    }
  });
}).listen(port, host);
