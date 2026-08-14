# GC sidecar

Resolves a CS2 sharecode to a demo download URL.

## Why this exists

The demo URL is only obtainable from the CS2 **game coordinator**. There is no
Steam Web API endpoint for it: a sharecode decodes to match/outcome/token
identifiers, but the `replayNNN.valve.net` address comes back from a Steam
client protocol conversation, not an HTTP call.

No maintained Go library speaks that protocol (`Philipp15b/go-steam` has no CS2
GC and no release since 2021; `k64z/steamstacks` implements the Steam CM
protocol and a *TF2* GC but not CS2). `node-globaloffensive` does, and is
actively maintained. So this is a deliberately thin Node service — one
endpoint, no business logic — and everything else stays in Go.

## Setup

It needs a **dedicated Steam account**. Never a personal one:

- The credentials live on the VM, and this container's dependency tree has
  known CVEs (see below).
- It should own nothing worth stealing: no inventory, no payment method, no
  friends list that matters.

The account must own CS2 (free) and be able to reach the GC.

### First login

Steam Guard needs answering once. Either:

- **Preferred:** set `STEAM_SHARED_SECRET` (the TOTP shared secret from the
  mobile authenticator). The container then reconnects unattended, including
  after a restart at an inconvenient hour.
- Or log in once somewhere interactive to obtain a refresh token and pass it as
  `STEAM_REFRESH_TOKEN`.

After a successful login the refresh token is written to `/data/refresh-token`
(mode 600, on a volume), so subsequent starts need no password at all.

### Environment

| Variable | Purpose |
|---|---|
| `STEAM_ACCOUNT_NAME` | Bot account login |
| `STEAM_PASSWORD` | Bot account password |
| `STEAM_SHARED_SECRET` | Optional TOTP secret; without it, restarts may need a human |
| `STEAM_REFRESH_TOKEN` | Optional; overrides the saved token |
| `GC_MIN_REQUEST_GAP_MS` | Minimum gap between GC requests (default 2000) |

## API

```
GET /demo-url?sharecode=CSGO-xxxxx-xxxxx-xxxxx-xxxxx-xxxxx
    200 {"demo_url": "http://replay...valve.net/730/....dem.bz2"}
    404 {"error": "..."}      demo expired, or no demo in the match info
    503 {"error": "..."}      not connected to the game coordinator

GET /healthz
    200 / 503 {"gc_connected": bool}
```

Requests are serialised and rate-limited. `requestGame` results arrive on a
shared `matchList` event with no correlation id, so overlapping requests could
not be told apart even if the GC tolerated them.

A 404 is an ordinary answer: Valve expires matchmaking demos, and old matches
return stats with no replay.

## Known dependency vulnerabilities

`npm audit` reports 5 findings (4 high, 1 critical), all transitive through
`steam-user`: `protobufjs` (code execution), `adm-zip` (memory exhaustion) and
`steam-appticket`. `npm audit fix` cannot resolve them without breaking
changes, and they are unavoidable while using the only maintained CS2 GC
client.

What actually reduces the risk:

- the account is disposable and holds nothing of value
- the container publishes no ports and is reachable only from the compose
  network
- it runs as a non-root user
- it holds no application data — losing it entirely costs a re-login

Worth re-checking whenever `steam-user` releases.
