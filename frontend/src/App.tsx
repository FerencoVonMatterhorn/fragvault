import { useEffect, useState } from "react";
import { Link, NavLink, Navigate, Route, Routes } from "react-router-dom";
import {
  getMe,
  loginUrl,
  logout,
  BackendUnavailableError,
  type Me,
  type Highlight,
  type RoundResult,
} from "./api";
import { HighlightList } from "./Highlights";
import { RoundTimeline } from "./RoundTimeline";
import MatchesPage from "./MatchesPage";
import MatchPage from "./MatchPage";
import ProfilePage from "./ProfilePage";
import SetupPage from "./SetupPage";

// "offline" is deliberately not fatal. The frontend is a static bundle served
// independently of the backend, so when the backend is down the page should
// still render and say so, rather than blanking out — the two are deployed
// as separate containers and fail separately.
type MeState = "loading" | "ready" | "error" | "offline";

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

  async function signOut() {
    try {
      await logout();
      // Only on success: leaving the UI signed in after a failed logout would
      // be an outright lie about whether the session still exists.
      setMe(null);
    } catch {
      // Staying signed in is the honest outcome of a failed sign-out.
    }
  }

  if (meState === "loading") return <div className="centered">Loading…</div>;
  // Reserved for the backend answering with something unexpected, which does
  // suggest a real bug rather than an absent service.
  if (meState === "error") return <div className="centered">Something went wrong talking to the backend.</div>;

  const offline = meState === "offline";

  return (
    <>
      <header className="masthead">
        <div className="masthead-left">
          <Link to="/" className="masthead-brand">
            FragVault
          </Link>
          {/* Only for signed-in users: every destination behind it needs a
              session, so showing it signed out would be four dead ends. */}
          {me && (
            <nav className="nav">
              <NavLink to="/matches" className={navClass}>
                Matches
              </NavLink>
              <NavLink to="/profile" className={navClass}>
                Profile
              </NavLink>
              <NavLink to="/setup" className={navClass}>
                Setup
              </NavLink>
            </nav>
          )}
        </div>

        {me && (
          <div className="masthead-user">
            {me.avatar_url && <img className="avatar" src={me.avatar_url} alt="" width={28} height={28} />}
            <span className="hide-narrow">{me.persona}</span>
            <button className="btn btn-secondary btn-small" onClick={signOut}>
              Sign out
            </button>
          </div>
        )}
      </header>

      <main className="page">
        {offline && me && <OfflineNotice />}
        <Routes>
          <Route path="/" element={me ? <Navigate to="/matches" replace /> : <SignedOut backendOffline={offline} />} />
          <Route path="/matches" element={me ? <MatchesPage /> : <SignedOut backendOffline={offline} />} />
          <Route
            path="/profile"
            element={me ? <ProfilePage me={me} onLoggedOut={signOut} /> : <SignedOut backendOffline={offline} />}
          />
          <Route path="/setup" element={me ? <SetupPage /> : <SignedOut backendOffline={offline} />} />
          <Route
            path="/match/:shareCode"
            element={me ? <MatchPage me={me} /> : <SignedOut backendOffline={offline} />}
          />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </main>
    </>
  );
}

function navClass({ isActive }: { isActive: boolean }) {
  return isActive ? "nav-link nav-link-active" : "nav-link";
}

function NotFound() {
  return (
    <div className="centered">
      <p>That page doesn't exist.</p>
      <Link to="/">Back to your matches</Link>
    </div>
  );
}

// The hero is the product's own artifact rather than a description of it: a
// real match card, with the round strip and the highlights it produces. Fixed
// sample data, but the exact shape a real one takes.
const SAMPLE_ROUNDS: RoundResult[] = [
  3, 3, 2, 3, 2, 2, 3, 3, 3, 2, 3, 3, 2, 3, 3, 2, 3, 3, 3, 2, 3, 2, 3, 3,
].map((winner, i) => ({ number: i + 1, winner, start_s: i * 110, end_s: i * 110 + 95 }));

const SAMPLE_HIGHLIGHTS: Highlight[] = [
  { steam_id: "", kind: "multi_kill", round: 7, start_s: 548, end_s: 581, score: 5, metadata: { kills: 5 } },
  { steam_id: "", kind: "clutch", round: 12, start_s: 1023, end_s: 1078, score: 7, metadata: { enemies_alive: 3 } },
  { steam_id: "", kind: "opening_duel", round: 3, start_s: 112, end_s: 121, score: 1, metadata: { weapon: "AK-47" } },
];

function SignedOut({ backendOffline = false }: { backendOffline?: boolean }) {
  return (
    <>
      <div className="section" style={{ marginTop: 64 }}>
        <h1>Paste one sharecode.</h1>
        <p className="muted" style={{ margin: "10px 0 0", fontSize: 19 }}>
          FragVault reads the demo and finds every ace, clutch and opening pick in it.
        </p>
      </div>

      <div className="card" style={{ marginTop: 28 }}>
        <div className="card-head" style={{ marginBottom: 14 }}>
          <div className="card-head-main">
            <img className="map-thumb" src="/maps/de_mirage.png" alt="" />
            <div>
              <h2>Mirage</h2>
              <div className="mono caption muted">CSGO-Pjh5A-pd5aa-tKzDo-XHj4o-nrXwP</div>
            </div>
          </div>
          <span className="scoreline">
            13<span className="muted">–</span>11
          </span>
        </div>

        <RoundTimeline rounds={SAMPLE_ROUNDS} highlights={SAMPLE_HIGHLIGHTS} />

        <div style={{ marginTop: 16 }}>
          <HighlightList highlights={SAMPLE_HIGHLIGHTS} />
        </div>
      </div>

      {backendOffline && (
        <div style={{ marginTop: 28 }}>
          <OfflineNotice />
        </div>
      )}

      <div style={{ marginTop: 28 }}>
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

function OfflineNotice() {
  return (
    <p className="notice" role="status">
      Can't reach the backend right now, so signing in and match history are unavailable. Nothing is wrong on your end —
      try again in a minute.
    </p>
  );
}
