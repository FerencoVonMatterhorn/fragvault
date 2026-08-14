import { useEffect, useState } from "react";
import {
  getMe,
  getMatches,
  submitOnboarding,
  loginUrl,
  logout,
  getAnalysis,
  analyzeMatch,
  BackendUnavailableError,
  type Me,
  type Match,
  type Analysis,
} from "./api";

type LoadState = "loading" | "ready" | "error";

// "offline" is deliberately not fatal. The frontend is a static bundle served
// independently of the backend, so when the backend is down the page should
// still render and say so, rather than blanking out — the two are deployed
// as separate containers and fail separately.
type MeState = LoadState | "offline";

export default function App() {
  const [me, setMe] = useState<Me | null>(null);
  const [meState, setMeState] = useState<MeState>("loading");

  useEffect(() => {
    getMe()
      .then((m) => {
        setMe(m);
        setMeState("ready");
      })
      .catch((err) => setMeState(err instanceof BackendUnavailableError ? "offline" : "error"));
  }, []);

  if (meState === "loading") return <div className="centered">Loading…</div>;
  // Reserved for the backend answering with something unexpected, which does
  // suggest a real bug rather than an absent service.
  if (meState === "error") return <div className="centered">Something went wrong talking to the backend.</div>;

  const offline = meState === "offline";

  return (
    <>
      <header className="masthead">
        <p className="masthead-brand">FragVault</p>
        {me && <SignedInAs me={me} onLoggedOut={() => setMe(null)} />}
      </header>

      <main className="page">
        {me ? (
          <SignedIn offline={offline} />
        ) : (
          <SignedOut backendOffline={offline} />
        )}
      </main>
    </>
  );
}

function SignedInAs({ me, onLoggedOut }: { me: Me; onLoggedOut: () => void }) {
  const [loggingOut, setLoggingOut] = useState(false);

  async function handleLogout() {
    setLoggingOut(true);
    try {
      await logout();
      // Only on success: leaving the UI signed in after a failed logout would
      // be an outright lie about whether the session still exists.
      onLoggedOut();
    } catch {
      setLoggingOut(false);
    }
  }

  return (
    <div className="masthead-user">
      {me.avatar_url && <img className="avatar" src={me.avatar_url} alt="" width={28} height={28} />}
      <span>{me.persona}</span>
      <button className="btn btn-secondary btn-small" onClick={handleLogout} disabled={loggingOut}>
        {loggingOut ? "Signing out…" : "Sign out"}
      </button>
    </div>
  );
}

function SignedOut({ backendOffline = false }: { backendOffline?: boolean }) {
  return (
    <>
      <div className="hero">
        <h1>Every round you played. The moments worth keeping.</h1>
        <p>Sign in with Steam and FragVault finds your best plays automatically.</p>
      </div>

      {backendOffline && <OfflineNotice />}

      <div style={{ textAlign: "center" }}>
        {/* Not wrapped in the login link while offline: /auth/steam/login goes
            through the same backend, so following it would only produce a 502. */}
        {backendOffline ? (
          <button className="btn" disabled>
            Sign in with Steam
          </button>
        ) : (
          <a href={loginUrl()}>
            <button className="btn">Sign in with Steam</button>
          </a>
        )}
      </div>
    </>
  );
}

function SignedIn({ offline }: { offline: boolean }) {
  return (
    <>
      {offline && <OfflineNotice />}
      <Onboarding />
      <MatchList />
    </>
  );
}

function OfflineNotice() {
  return (
    <p className="notice" role="status">
      Can't reach the backend right now, so signing in and match history are unavailable. Nothing is wrong on your end —
      try again in a minute.
    </p>
  );
}

function Onboarding() {
  const [authCode, setAuthCode] = useState("");
  const [startingShareCode, setStartingShareCode] = useState("");
  const [status, setStatus] = useState<"idle" | "saving" | "saved" | "error">("idle");
  const [errorMsg, setErrorMsg] = useState("");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setStatus("saving");
    try {
      await submitOnboarding(authCode, startingShareCode);
      setStatus("saved");
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : String(err));
      setStatus("error");
    }
  }

  return (
    <details className="panel section">
      <summary>Connect your match history</summary>
      <p className="small muted">
        Get your game authentication code from{" "}
        <a
          href="https://help.steampowered.com/en/wizard/HelpWithGameIssue?appid=730&issueid=128"
          target="_blank"
          rel="noreferrer"
        >
          Steam Support
        </a>
        , and a sharecode from CS2's match history. Use an <strong>older</strong> match — discovery walks forward from
        the code you give it, so your most recent one finds nothing.
      </p>
      <form onSubmit={submit} className="stack" style={{ maxWidth: 380, marginTop: 16 }}>
        <input
          className="field"
          placeholder="Game auth code"
          value={authCode}
          onChange={(e) => setAuthCode(e.target.value)}
        />
        <input
          className="field"
          placeholder="Starting sharecode (CSGO-…)"
          value={startingShareCode}
          onChange={(e) => setStartingShareCode(e.target.value)}
        />
        <div>
          <button className="btn" type="submit" disabled={status === "saving"}>
            {status === "saving" ? "Saving…" : "Save"}
          </button>
        </div>
        {status === "saved" && <p className="small success">Saved — refresh your matches below.</p>}
        {status === "error" && <p className="small error">{errorMsg}</p>}
      </form>
    </details>
  );
}

function MatchList() {
  const [matches, setMatches] = useState<Match[] | null>(null);
  const [state, setState] = useState<LoadState>("loading");
  const [errorMsg, setErrorMsg] = useState("");

  function load() {
    setState("loading");
    getMatches()
      .then((m) => {
        setMatches(m);
        setState("ready");
      })
      .catch((err) => {
        setErrorMsg(err instanceof Error ? err.message : String(err));
        setState("error");
      });
  }

  useEffect(load, []);

  return (
    <section className="section">
      <div className="section-head">
        <h2>Recent matches</h2>
        <button className="btn btn-secondary btn-small" onClick={load} disabled={state === "loading"}>
          {state === "loading" ? "Checking…" : "Refresh"}
        </button>
      </div>

      {state === "loading" && <p className="small muted">Checking for matches…</p>}
      {state === "error" && <p className="small error">{errorMsg}</p>}
      {state === "ready" && matches && matches.length === 0 && (
        <p className="small muted">No matches discovered yet.</p>
      )}
      {state === "ready" && matches && matches.length > 0 && (
        <ul className="list-reset">
          {[...matches].reverse().map((m) => (
            <MatchRow key={m.share_code} match={m} />
          ))}
        </ul>
      )}
    </section>
  );
}

const KIND_LABELS: Record<string, string> = {
  multi_kill: "Multi-kill",
  clutch: "Clutch",
  opening_duel: "Opening duel",
  defuse: "Defuse",
};

function MatchRow({ match }: { match: Match }) {
  const [analysis, setAnalysis] = useState<Analysis | null>(null);
  const [demoUrl, setDemoUrl] = useState("");
  const [showForm, setShowForm] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    getAnalysis(match.share_code)
      .then((a) => {
        if (!cancelled) setAnalysis(a);
      })
      .catch(() => {
        // A match with no analysis is the normal case, and a failure here
        // shouldn't take out the match list.
      });
    return () => {
      cancelled = true;
    };
  }, [match.share_code]);

  const status = analysis?.status ?? "none";
  const working = status === "pending" || status === "running";

  // Parsing takes minutes, so the row polls itself while work is in flight
  // and stops as soon as it isn't.
  useEffect(() => {
    if (!working) return;
    const id = setInterval(() => {
      getAnalysis(match.share_code)
        .then(setAnalysis)
        .catch(() => {});
    }, 4000);
    return () => clearInterval(id);
  }, [working, match.share_code]);

  async function queue(url?: string) {
    setSubmitting(true);
    setError("");
    try {
      setAnalysis(await analyzeMatch(match.share_code, url));
      setShowForm(false);
      setDemoUrl("");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      // Automatic lookup failed — most often an expired demo. Offer the
      // manual route rather than leaving a dead end.
      if (!url) setShowForm(true);
    } finally {
      setSubmitting(false);
    }
  }

  function submit(e: React.FormEvent) {
    e.preventDefault();
    void queue(demoUrl.trim());
  }

  return (
    <li className="card">
      <div className="card-head">
        <div>
          <div className="mono">{match.share_code}</div>
          <div className="small muted">
            {new Date(match.discovered_at).toLocaleDateString(undefined, {
              day: "numeric",
              month: "short",
              year: "numeric",
            })}
            {analysis?.map_name && ` · ${analysis.map_name}`}
          </div>
        </div>

        <div>
          {status === "none" && !showForm && (
            <button className="btn btn-secondary btn-small" onClick={() => void queue()} disabled={submitting}>
              {submitting ? "Looking up demo…" : "Analyse"}
            </button>
          )}
          {working && <span className="status">Analysing…</span>}
          {status === "done" && (
            <span className="status status-done">
              {analysis?.highlights.length ?? 0} highlight{(analysis?.highlights.length ?? 0) === 1 ? "" : "s"}
            </span>
          )}
          {status === "failed" && <span className="status status-failed">Unavailable</span>}
        </div>
      </div>

      {showForm && (
        <form onSubmit={submit} style={{ marginTop: 16 }}>
          <p className="small muted" style={{ margin: "0 0 10px" }}>
            Paste a demo URL directly. Useful when the match is too old for Valve to still have the demo.
          </p>
          <div className="row">
            <input
              className="field"
              placeholder="Demo URL (.dem or .dem.bz2)"
              value={demoUrl}
              onChange={(e) => setDemoUrl(e.target.value)}
            />
            <button className="btn btn-small" type="submit" disabled={submitting || demoUrl.trim() === ""}>
              {submitting ? "Queuing…" : "Queue"}
            </button>
            <button
              className="btn btn-secondary btn-small"
              type="button"
              onClick={() => setShowForm(false)}
              disabled={submitting}
            >
              Cancel
            </button>
          </div>
        </form>
      )}

      {error && <p className="small error">{error}</p>}

      {/* Valve expires demos, so a failure here is usually "too old" rather
          than anything broken. */}
      {status === "failed" && analysis?.error && <p className="small error">{analysis.error}</p>}

      {status === "done" && analysis && analysis.highlights.length > 0 && (
        <ol className="highlights">
          {analysis.highlights.map((h, i) => (
            <li className="highlight" key={`${h.kind}-${h.round}-${h.start_s}-${i}`}>
              <span>
                <span className="highlight-kind">{KIND_LABELS[h.kind] ?? h.kind}</span>
                <span className="muted"> · round {h.round}</span>
              </span>
              <span className="muted mono">
                {formatClock(h.start_s)}–{formatClock(h.end_s)}
              </span>
            </li>
          ))}
        </ol>
      )}

      {status === "done" && analysis?.highlights.length === 0 && (
        <p className="small muted" style={{ marginBottom: 0 }}>
          Parsed successfully, but nothing met the highlight thresholds.
        </p>
      )}
    </li>
  );
}

function formatClock(seconds: number): string {
  const total = Math.max(0, Math.round(seconds));
  const mins = Math.floor(total / 60);
  const secs = total % 60;
  return `${mins}:${secs.toString().padStart(2, "0")}`;
}
