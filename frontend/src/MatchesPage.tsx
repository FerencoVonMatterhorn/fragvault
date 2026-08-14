import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { getMatches, getAnalysis, analyzeMatch, type Match, type Analysis, type Me } from "./api";
import { mapDisplayName, mapImageUrl } from "./maps";
import { RoundTimeline } from "./RoundTimeline";

type LoadState = "loading" | "ready" | "error";

export default function MatchesPage({ me }: { me: Me }) {
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
        <h1 style={{ fontSize: 32, fontWeight: 600, letterSpacing: "-0.012em", margin: 0 }}>Match history</h1>
        <button className="btn btn-secondary btn-small" onClick={load} disabled={state === "loading"}>
          {state === "loading" ? "Checking…" : "Refresh"}
        </button>
      </div>

      {state === "loading" && <p className="small muted">Checking for matches…</p>}
      {state === "error" && (
        <p className="small error">
          {errorMsg}{" "}
          <Link to="/setup">Connect your match history</Link> if you haven't yet.
        </p>
      )}
      {state === "ready" && matches && matches.length === 0 && (
        <p className="small muted">
          No matches discovered yet. Check <Link to="/setup">Setup</Link> — discovery walks forward from the sharecode
          you gave, so an older one finds more.
        </p>
      )}
      {state === "ready" && matches && matches.length > 0 && (
        <ul className="list-reset">
          {[...matches].reverse().map((m) => (
            <MatchRow key={m.share_code} match={m} me={me} />
          ))}
        </ul>
      )}
    </section>
  );
}

function MatchRow({ match, me }: { match: Match; me: Me }) {
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
  const mapImage = mapImageUrl(analysis?.map_name);
  const myHighlights = analysis?.highlights.filter((h) => h.steam_id === me.steam_id).length ?? 0;

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
        <div className="card-head-main">
          {/* Only appears once the map is known, which is after analysis —
              before that there is nothing to show a picture of. */}
          {mapImage && <img className="map-thumb" src={mapImage} alt="" loading="lazy" />}
          <div style={{ minWidth: 0 }}>
            {analysis?.map_name ? (
              <h2 style={{ fontSize: "var(--t-small)" }}>{mapDisplayName(analysis.map_name)}</h2>
            ) : (
              <div className="mono caption">{match.share_code}</div>
            )}
            <div className="caption muted">
              {new Date(match.discovered_at).toLocaleDateString(undefined, {
                day: "numeric",
                month: "short",
                year: "numeric",
              })}
            </div>
            {/* A condensed strip in the row: how the match went, before you
                open it. */}
            {analysis && analysis.rounds.length > 0 && (
              <div style={{ marginTop: 8 }}>
                <RoundTimeline rounds={analysis.rounds} small />
              </div>
            )}
          </div>
        </div>

        <div>
          {status === "none" && !showForm && (
            <button className="btn btn-secondary btn-small" onClick={() => void queue()} disabled={submitting}>
              {submitting ? "Looking up demo…" : "Analyse"}
            </button>
          )}
          {working && <span className="status">Analysing…</span>}
          {/* The detail page is where the scoreboard and highlights live, so
              a finished analysis is a link rather than an inline dump. */}
          {/* Counts yours, not the whole server's — an enemy's ace is not
              something this app is offering you. */}
          {status === "done" && (
            <Link to={`/match/${encodeURIComponent(match.share_code)}`} className="small">
              {myHighlights} highlight{myHighlights === 1 ? "" : "s"} ›
            </Link>
          )}
          {/* Failures are often transient — the game coordinator was down,
              the download stalled — so a retry is a button, not a support
              ticket. */}
          {status === "failed" && !showForm && (
            <button className="btn btn-secondary btn-small" onClick={() => void queue()} disabled={submitting}>
              {submitting ? "Retrying…" : "Retry"}
            </button>
          )}
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
      {status === "failed" && analysis?.error && (
        <p className="small error" style={{ marginBottom: 0 }}>
          {analysis.error}{" "}
          {!showForm && (
            <button className="linklike" onClick={() => setShowForm(true)}>
              Use a demo URL instead
            </button>
          )}
        </p>
      )}
    </li>
  );
}
