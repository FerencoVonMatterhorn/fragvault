"use strict";

// Resolves a CS2 sharecode to a demo download URL.
//
// This exists because the demo URL is only obtainable from the CS2 game
// coordinator — there is no Steam Web API endpoint for it — and no maintained
// Go library speaks that protocol. So the Go backend asks this over the
// compose network and everything else stays in Go.
//
// Deliberately thin: one endpoint, no state beyond a Steam session, and no
// business logic. When Valve changes the GC, the fix should be a dependency
// bump rather than our code.

const fs = require("fs");
const http = require("http");
const path = require("path");

const SteamUser = require("steam-user");
const SteamTotp = require("steam-totp");
const GlobalOffensive = require("globaloffensive");

const PORT = Number(process.env.PORT || 3001);
const TOKEN_PATH = process.env.STEAM_TOKEN_PATH || "/data/refresh-token";

// Minimum gap between game coordinator requests. The GC is not ours to
// hammer, and an account that hammers it gets rate-limited or worse.
const MIN_REQUEST_GAP_MS = Number(process.env.GC_MIN_REQUEST_GAP_MS || 2000);
const REQUEST_TIMEOUT_MS = Number(process.env.GC_REQUEST_TIMEOUT_MS || 30000);

const client = new SteamUser();
const csgo = new GlobalOffensive(client);

let gcReady = false;
let lastRequestAt = 0;

// One in-flight GC request at a time. requestGame results arrive on a shared
// 'matchList' event with no correlation id, so overlapping requests could not
// be told apart.
let queue = Promise.resolve();

function log(...args) {
  console.log(new Date().toISOString(), ...args);
}

function logOn() {
  const saved = readSavedToken();
  if (saved) {
    log("logging on with a saved refresh token");
    client.logOn({ refreshToken: saved });
    return;
  }

  const accountName = process.env.STEAM_ACCOUNT_NAME;
  const password = process.env.STEAM_PASSWORD;
  if (!accountName || !password) {
    console.error(
      "no saved refresh token and no STEAM_ACCOUNT_NAME/STEAM_PASSWORD — cannot log on"
    );
    process.exit(1);
  }

  const details = { accountName, password };
  // A shared secret lets the container reconnect unattended. Without one,
  // Steam Guard needs a human, which is fine for first login but not for a
  // service that must survive a restart at 3am.
  if (process.env.STEAM_SHARED_SECRET) {
    details.twoFactorCode = SteamTotp.generateAuthCode(process.env.STEAM_SHARED_SECRET);
  }
  log(`logging on as ${accountName}`);
  client.logOn(details);
}

function readSavedToken() {
  if (process.env.STEAM_REFRESH_TOKEN) {
    return process.env.STEAM_REFRESH_TOKEN;
  }
  try {
    const token = fs.readFileSync(TOKEN_PATH, "utf8").trim();
    return token || null;
  } catch {
    return null;
  }
}

client.on("refreshToken", (token) => {
  // Saved so restarts don't need the password, and so Steam Guard is a
  // one-time setup step rather than a recurring interruption.
  try {
    fs.mkdirSync(path.dirname(TOKEN_PATH), { recursive: true });
    fs.writeFileSync(TOKEN_PATH, token, { mode: 0o600 });
    log("saved a new refresh token");
  } catch (err) {
    log("warning: could not persist refresh token:", err.message);
  }
});

client.on("steamGuard", (domain, callback, lastCodeWrong) => {
  // Nothing here can answer this. Failing loudly beats hanging forever
  // pretending to be healthy.
  console.error(
    `Steam Guard code required${domain ? ` (emailed to ${domain})` : ""}${
      lastCodeWrong ? " — the previous code was wrong" : ""
    }.`
  );
  console.error("Log in once interactively to produce a refresh token:");
  console.error("  docker compose run --rm -it gc-sidecar node login.js");
  console.error("Accounts with a mobile authenticator can set STEAM_SHARED_SECRET instead.");
  process.exit(1);
});

client.on("error", (err) => {
  // steam-user crashes the process if this is unhandled. An explicit exit
  // with the reason is more useful, and the restart policy handles recovery.
  console.error("steam error:", err.message);
  process.exit(1);
});

client.on("loggedOn", () => {
  log("logged on to Steam");
  // Invisible: this is a service account, and it appearing online in friends
  // lists is noise at best.
  client.setPersona(SteamUser.EPersonaState.Offline);
  client.gamesPlayed([730]);
});

csgo.on("connectedToGC", () => {
  gcReady = true;
  log("connected to the CS2 game coordinator");
});

csgo.on("disconnectedFromGC", (reason) => {
  gcReady = false;
  log("disconnected from the game coordinator:", reason);
});

// Walks the match structure looking for a demo URL.
//
// Deliberately structural rather than reading a fixed path: the GC response
// shape is undocumented and has changed before, and "the string that looks
// like a demo URL" is far more stable than roundstatsall[N].map.
function findDemoUrl(value, depth = 0) {
  if (depth > 8 || value == null) return null;

  if (typeof value === "string") {
    return /^https?:\/\/\S+\.dem(\.bz2|\.gz)?$/i.test(value) ? value : null;
  }
  if (Array.isArray(value)) {
    // Last first: the final round's stats carry the complete match's demo.
    for (let i = value.length - 1; i >= 0; i--) {
      const found = findDemoUrl(value[i], depth + 1);
      if (found) return found;
    }
    return null;
  }
  if (typeof value === "object") {
    for (const key of Object.keys(value)) {
      const found = findDemoUrl(value[key], depth + 1);
      if (found) return found;
    }
  }
  return null;
}

function requestDemoUrl(shareCode) {
  return new Promise((resolve, reject) => {
    let settled = false;

    const timer = setTimeout(() => {
      if (settled) return;
      settled = true;
      csgo.removeListener("matchList", onMatchList);
      reject(new Error("game coordinator did not respond in time"));
    }, REQUEST_TIMEOUT_MS);

    function onMatchList(matches, data) {
      if (settled) return;
      const url = findDemoUrl(matches) || findDemoUrl(data);
      if (!url) {
        // A match with no demo is normal: Valve expires them, and very old
        // matches come back with stats but no replay.
        settled = true;
        clearTimeout(timer);
        csgo.removeListener("matchList", onMatchList);
        reject(new Error("no demo URL in the match info — the demo has most likely expired"));
        return;
      }
      settled = true;
      clearTimeout(timer);
      csgo.removeListener("matchList", onMatchList);
      resolve(url);
    }

    csgo.on("matchList", onMatchList);

    try {
      csgo.requestGame(shareCode);
    } catch (err) {
      settled = true;
      clearTimeout(timer);
      csgo.removeListener("matchList", onMatchList);
      reject(err);
    }
  });
}

// Serialises requests and enforces the minimum gap between them.
function enqueue(shareCode) {
  const run = queue.then(async () => {
    const wait = Math.max(0, lastRequestAt + MIN_REQUEST_GAP_MS - Date.now());
    if (wait > 0) await new Promise((r) => setTimeout(r, wait));
    lastRequestAt = Date.now();
    return requestDemoUrl(shareCode);
  });
  // Keep the chain alive regardless of this request's outcome.
  queue = run.catch(() => {});
  return run;
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, "http://localhost");

  if (url.pathname === "/healthz") {
    res.writeHead(gcReady ? 200 : 503, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ gc_connected: gcReady }));
    return;
  }

  if (url.pathname === "/demo-url" && req.method === "GET") {
    const shareCode = url.searchParams.get("sharecode");
    if (!shareCode) {
      res.writeHead(400, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "sharecode is required" }));
      return;
    }
    if (!gcReady) {
      res.writeHead(503, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "not connected to the game coordinator" }));
      return;
    }

    try {
      const demoUrl = await enqueue(shareCode);
      log(`resolved ${shareCode}`);
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ demo_url: demoUrl }));
    } catch (err) {
      log(`failed to resolve ${shareCode}: ${err.message}`);
      // 404 rather than 500: an expired demo is an ordinary answer, and the
      // backend records it as a failed analysis with this reason.
      res.writeHead(404, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: err.message }));
    }
    return;
  }

  res.writeHead(404, { "Content-Type": "application/json" });
  res.end(JSON.stringify({ error: "not found" }));
});

server.listen(PORT, () => log(`gc sidecar listening on :${PORT}`));

logOn();
