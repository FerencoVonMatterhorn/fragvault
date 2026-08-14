"use strict";

// One-time interactive login.
//
// Run this when the bot account uses email Steam Guard, which cannot be
// automated the way a mobile authenticator's shared secret can. It logs in
// once with a code you type, saves the resulting refresh token to the volume,
// and exits. The service then starts unattended from that token — including
// after restarts, since the token outlives the session.
//
//   docker compose run --rm -it gc-sidecar node login.js
//
// The --rm -it matters: without a TTY there is nothing to type the code into.

const fs = require("fs");
const path = require("path");
const readline = require("readline");

const SteamUser = require("steam-user");

const TOKEN_PATH = process.env.STEAM_TOKEN_PATH || "/data/refresh-token";

const accountName = process.env.STEAM_ACCOUNT_NAME;
const password = process.env.STEAM_PASSWORD;

if (!accountName || !password) {
  console.error("STEAM_ACCOUNT_NAME and STEAM_PASSWORD must be set (they come from .env)");
  process.exit(1);
}

const client = new SteamUser();
let saved = false;

client.on("steamGuard", (domain, callback) => {
  const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
  const where = domain ? `emailed to ${domain}` : "from your authenticator";
  // Email codes expire quickly, so this is worth typing promptly.
  rl.question(`Steam Guard code (${where}): `, (code) => {
    rl.close();
    callback(code.trim());
  });
});

client.on("refreshToken", (token) => {
  try {
    fs.mkdirSync(path.dirname(TOKEN_PATH), { recursive: true });
    fs.writeFileSync(TOKEN_PATH, token, { mode: 0o600 });
    saved = true;
    console.log(`saved refresh token to ${TOKEN_PATH}`);
  } catch (err) {
    console.error("could not write the refresh token:", err.message);
    process.exit(1);
  }
});

client.on("loggedOn", () => {
  console.log("logged on successfully");
  // The token arrives around the same time as this event; give it a moment
  // rather than racing it, then confirm what actually happened.
  setTimeout(() => {
    if (!saved) {
      console.error(
        "logged on but no refresh token was issued — check that the volume is writable"
      );
      process.exit(1);
    }
    console.log("done — start the service normally and it will use this token");
    client.logOff();
    process.exit(0);
  }, 2000);
});

client.on("error", (err) => {
  console.error("login failed:", err.message);
  process.exit(1);
});

console.log(`logging in as ${accountName}…`);
client.logOn({ accountName, password });
