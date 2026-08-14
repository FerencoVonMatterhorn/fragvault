import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { getMatches, type Me } from "./api";

export default function ProfilePage({ me, onLoggedOut }: { me: Me; onLoggedOut: () => void }) {
  const [matchCount, setMatchCount] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    getMatches()
      .then((m) => {
        if (!cancelled) setMatchCount(m.length);
      })
      .catch(() => {
        // Not onboarded yet returns an error here, which is a normal state
        // rather than something to shout about — the count simply stays
        // unknown.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <section className="section">
      <div className="profile-head">
        {me.avatar_url && <img className="profile-avatar" src={me.avatar_url} alt="" width={88} height={88} />}
        <div style={{ minWidth: 0 }}>
          <h1 style={{ fontSize: 32, fontWeight: 600, letterSpacing: "-0.012em", margin: "0 0 4px" }}>{me.persona}</h1>
          <p className="small muted mono" style={{ margin: "0 0 8px" }}>
            {me.steam_id}
          </p>
          <a
            className="small"
            href={`https://steamcommunity.com/profiles/${me.steam_id}`}
            target="_blank"
            rel="noreferrer"
          >
            View Steam profile ↗
          </a>
        </div>
      </div>

      <div className="panel" style={{ marginTop: 28 }}>
        <div className="stat-row">
          <span className="muted">Matches discovered</span>
          <span className="stat-value">{matchCount ?? "—"}</span>
        </div>
        <p className="small muted" style={{ margin: "12px 0 0" }}>
          {matchCount === null
            ? "Connect your match history in Setup to start discovering matches."
            : "Discovery walks forward from the sharecode you provided during setup."}
        </p>
      </div>

      {/* Setup lives here rather than in the navigation: it's a one-time task,
          but the Valve auth code expires, so it still has to be findable. */}
      <div className="section">
        <h2>Match history connection</h2>
        <p className="small muted">
          FragVault reads your matches using a Valve authentication code you provide. It expires periodically, and
          discovery stops until you replace it.
        </p>
        <div className="row" style={{ marginTop: 12 }}>
          <Link to="/setup" className="btn btn-secondary btn-small">
            Update connection
          </Link>
        </div>
      </div>

      <div className="section">
        <h2>Account</h2>
        <p className="small muted">
          Signing out clears the session cookie on this device. Your connected match history stays connected.
        </p>
        <div className="row" style={{ marginTop: 12 }}>
          <button className="btn btn-secondary btn-small" onClick={onLoggedOut}>
            Sign out
          </button>
        </div>
      </div>
    </section>
  );
}
