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

  if (meState === "loading") return <Centered>Loading…</Centered>;
  // Reserved for the backend answering with something unexpected, which does
  // suggest a real bug rather than an absent service.
  if (meState === "error") return <Centered>Something went wrong talking to the backend.</Centered>;

  const offline = meState === "offline";

  return (
    <div style={{ maxWidth: 640, margin: "4rem auto", fontFamily: "system-ui, sans-serif" }}>
      <h1>FragVault</h1>
      {offline && <OfflineNotice />}
      {me ? <LoggedIn me={me} onLoggedOut={() => setMe(null)} /> : <LoggedOut backendOffline={offline} />}
    </div>
  );
}

function OfflineNotice() {
  return (
    <p
      role="status"
      style={{
        background: "#fff4e5",
        border: "1px solid #ffb74d",
        borderRadius: 8,
        padding: "0.75rem 1rem",
        fontSize: "0.95rem",
      }}
    >
      Can't reach the backend right now, so signing in and match history are unavailable. Nothing is wrong on your end —
      try again in a minute.
    </p>
  );
}

function LoggedOut({ backendOffline = false }: { backendOffline?: boolean }) {
  const buttonStyle = { padding: "0.6rem 1.2rem", fontSize: "1rem" };

  return (
    <div>
      <p>Sign in with Steam to see your recent CS2 matches.</p>
      {/* Not wrapped in the login link while offline: /auth/steam/login goes
          through the same backend, so following it would only produce a 502. */}
      {backendOffline ? (
        <button style={buttonStyle} disabled>
          Log in with Steam
        </button>
      ) : (
        <a href={loginUrl()}>
          <button style={buttonStyle}>Log in with Steam</button>
        </a>
      )}
    </div>
  );
}

function LoggedIn({ me, onLoggedOut }: { me: Me; onLoggedOut: () => void }) {
  const [loggingOut, setLoggingOut] = useState(false);
  const [logoutError, setLogoutError] = useState("");

  async function handleLogout() {
    setLoggingOut(true);
    setLogoutError("");
    try {
      await logout();
      // Only on success: leaving the UI signed in after a failed logout would
      // be an outright lie about whether the session still exists.
      onLoggedOut();
    } catch (err) {
      setLogoutError(err instanceof Error ? err.message : String(err));
      setLoggingOut(false);
    }
  }

  return (
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: "1rem" }}>
        <p style={{ margin: 0 }}>
          Signed in as <strong>{me.persona}</strong>
          {me.avatar_url && (
            <img src={me.avatar_url} alt="" width={32} height={32} style={{ verticalAlign: "middle", marginLeft: 8 }} />
          )}
        </p>
        <button onClick={handleLogout} disabled={loggingOut}>
          {loggingOut ? "Logging out…" : "Log out"}
        </button>
      </div>
      {logoutError && <p style={{ color: "crimson" }}>Couldn't log out: {logoutError}</p>}
      <Onboarding />
      <MatchList />
    </div>
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
    <details style={{ margin: "1.5rem 0", border: "1px solid #ccc", borderRadius: 8, padding: "0.75rem 1rem" }}>
      <summary style={{ cursor: "pointer", fontWeight: 600 }}>One-time setup: connect your match history</summary>
      <p style={{ fontSize: "0.9rem", color: "#444" }}>
        Get your game authentication code from{" "}
        <a href="https://help.steampowered.com/en/wizard/HelpWithGameIssue?appid=730&issueid=128" target="_blank" rel="noreferrer">
          Steam Support
        </a>{" "}
        and a starting sharecode from CS2's in-game match history settings, then paste both below. This only needs to be
        done once.
      </p>
      <form onSubmit={submit} style={{ display: "grid", gap: "0.5rem", maxWidth: 360 }}>
        <input placeholder="Game auth code" value={authCode} onChange={(e) => setAuthCode(e.target.value)} />
        <input
          placeholder="Starting sharecode (CSGO-...)"
          value={startingShareCode}
          onChange={(e) => setStartingShareCode(e.target.value)}
        />
        <button type="submit" disabled={status === "saving"}>
          Save
        </button>
        {status === "saved" && <p style={{ color: "green" }}>Saved — refresh matches below.</p>}
        {status === "error" && <p style={{ color: "crimson" }}>{errorMsg}</p>}
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
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2>Recent matches</h2>
        <button onClick={load} disabled={state === "loading"}>
          Refresh
        </button>
      </div>
      {state === "loading" && <p>Checking for matches…</p>}
      {state === "error" && <p style={{ color: "crimson" }}>{errorMsg}</p>}
      {state === "ready" && matches && matches.length === 0 && <p>No matches discovered yet.</p>}
      {state === "ready" && matches && matches.length > 0 && (
        <ul style={{ listStyle: "none", padding: 0 }}>
          {[...matches].reverse().map((m) => (
            <MatchRow key={m.share_code} match={m} />
          ))}
        </ul>
      )}
    </div>
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

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      setAnalysis(await analyzeMatch(match.share_code, demoUrl.trim()));
      setShowForm(false);
      setDemoUrl("");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <li style={{ border: "1px solid #ddd", borderRadius: 8, padding: "0.75rem 1rem", marginBottom: "0.75rem" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: "1rem" }}>
        <div>
          <code>{match.share_code}</code>
          <div style={{ fontSize: "0.85rem", color: "#666" }}>
            discovered {new Date(match.discovered_at).toLocaleString()}
            {analysis?.map_name && ` · ${analysis.map_name}`}
          </div>
        </div>
        <div style={{ fontSize: "0.9rem", whiteSpace: "nowrap" }}>
          {status === "none" && !showForm && <button onClick={() => setShowForm(true)}>Analyse</button>}
          {working && <span style={{ color: "#666" }}>Analysing…</span>}
          {status === "done" && <span>{analysis?.highlights.length ?? 0} highlights</span>}
          {status === "failed" && <span style={{ color: "crimson" }}>Failed</span>}
        </div>
      </div>

      {showForm && (
        <form onSubmit={submit} style={{ display: "flex", gap: "0.5rem", marginTop: "0.75rem" }}>
          <input
            style={{ flex: 1 }}
            placeholder="Demo URL (.dem or .dem.bz2)"
            value={demoUrl}
            onChange={(e) => setDemoUrl(e.target.value)}
          />
          <button type="submit" disabled={submitting || demoUrl.trim() === ""}>
            {submitting ? "Queuing…" : "Queue"}
          </button>
          <button type="button" onClick={() => setShowForm(false)} disabled={submitting}>
            Cancel
          </button>
        </form>
      )}

      {error && <p style={{ color: "crimson", fontSize: "0.9rem" }}>{error}</p>}

      {/* Valve expires demos, so a failure here is usually "too old" rather
          than anything broken. */}
      {status === "failed" && analysis?.error && (
        <p style={{ color: "crimson", fontSize: "0.85rem", marginBottom: 0 }}>{analysis.error}</p>
      )}

      {status === "done" && analysis && analysis.highlights.length > 0 && (
        <ol style={{ marginTop: "0.75rem", marginBottom: 0, fontSize: "0.9rem" }}>
          {analysis.highlights.map((h, i) => (
            <li key={`${h.kind}-${h.round}-${h.start_s}-${i}`}>
              <strong>{KIND_LABELS[h.kind] ?? h.kind}</strong> — round {h.round}, {formatClock(h.start_s)}–
              {formatClock(h.end_s)}
            </li>
          ))}
        </ol>
      )}

      {status === "done" && analysis?.highlights.length === 0 && (
        <p style={{ fontSize: "0.85rem", color: "#666", marginBottom: 0 }}>
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

function Centered({ children }: { children: React.ReactNode }) {
  return <div style={{ textAlign: "center", marginTop: "4rem", fontFamily: "system-ui, sans-serif" }}>{children}</div>;
}
