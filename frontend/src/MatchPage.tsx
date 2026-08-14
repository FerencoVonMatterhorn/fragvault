import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { getAnalysis, type Analysis, type Me, type ScoreboardRow } from "./api";
import { HighlightList } from "./Highlights";

// CS2 team ids.
const TEAM_T = 2;
const TEAM_CT = 3;

type Tab = "scoreboard" | "highlights";

export default function MatchPage({ me }: { me: Me }) {
  const { shareCode = "" } = useParams();
  const [analysis, setAnalysis] = useState<Analysis | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [errorMsg, setErrorMsg] = useState("");
  // A view toggle, not a route: putting it in the URL would mean handling an
  // invalid tab name for no benefit.
  const [tab, setTab] = useState<Tab>("scoreboard");

  useEffect(() => {
    let cancelled = false;
    getAnalysis(shareCode)
      .then((a) => {
        if (cancelled) return;
        setAnalysis(a);
        setState("ready");
      })
      .catch((err) => {
        if (cancelled) return;
        setErrorMsg(err instanceof Error ? err.message : String(err));
        setState("error");
      });
    return () => {
      cancelled = true;
    };
  }, [shareCode]);

  if (state === "loading") return <div className="centered">Loading match…</div>;
  if (state === "error") return <div className="centered">{errorMsg}</div>;

  const done = analysis?.status === "done";

  return (
    <>
      <p className="section" style={{ marginBottom: 8 }}>
        <Link to="/" className="small">
          ‹ All matches
        </Link>
      </p>

      <header style={{ marginBottom: 28 }}>
        <h1 style={{ fontSize: 32, fontWeight: 600, letterSpacing: "-0.012em", margin: "0 0 6px" }}>
          {analysis?.map_name || "Match"}
        </h1>
        <p className="small muted mono" style={{ margin: 0 }}>
          {shareCode}
        </p>
        {done && (
          <p className="scoreline">
            {analysis.team_a_score} <span className="muted">–</span> {analysis.team_b_score}
          </p>
        )}
      </header>

      {!done && (
        <p className="notice">
          {analysis?.status === "failed"
            ? analysis.error || "This demo could not be analysed."
            : "This match hasn't been analysed yet."}
        </p>
      )}

      {done && (
        <>
          <nav className="tabs">
            <button
              className={`tab${tab === "scoreboard" ? " tab-active" : ""}`}
              onClick={() => setTab("scoreboard")}
            >
              Scoreboard
            </button>
            <button
              className={`tab${tab === "highlights" ? " tab-active" : ""}`}
              onClick={() => setTab("highlights")}
            >
              Highlights <span className="muted">{analysis.highlights.length}</span>
            </button>
          </nav>

          {tab === "scoreboard" ? (
            <Scoreboard rows={analysis.scoreboard} meSteamId={me.steam_id} />
          ) : (
            <HighlightList highlights={analysis.highlights} />
          )}
        </>
      )}
    </>
  );
}

function Scoreboard({ rows, meSteamId }: { rows: ScoreboardRow[]; meSteamId: string }) {
  if (rows.length === 0) {
    return <p className="small muted">No scoreboard data for this match.</p>;
  }

  // Already sorted by kills server-side; splitting preserves that order.
  const t = rows.filter((r) => r.team === TEAM_T);
  const ct = rows.filter((r) => r.team === TEAM_CT);
  // Anyone whose team we couldn't resolve still deserves to be listed rather
  // than silently dropped.
  const other = rows.filter((r) => r.team !== TEAM_T && r.team !== TEAM_CT);

  return (
    <>
      {ct.length > 0 && <TeamTable title="Counter-Terrorists" rows={ct} meSteamId={meSteamId} />}
      {t.length > 0 && <TeamTable title="Terrorists" rows={t} meSteamId={meSteamId} />}
      {other.length > 0 && <TeamTable title="Unassigned" rows={other} meSteamId={meSteamId} />}
    </>
  );
}

function TeamTable({ title, rows, meSteamId }: { title: string; rows: ScoreboardRow[]; meSteamId: string }) {
  return (
    <section style={{ marginBottom: 32 }}>
      <h2 style={{ fontSize: 19, marginBottom: 10 }}>{title}</h2>
      <div className="table-scroll">
        <table className="scoreboard">
          <thead>
            <tr>
              <th>Player</th>
              <th className="num">K</th>
              <th className="num">A</th>
              <th className="num">D</th>
              <th className="num">ADR</th>
              <th className="num">HS%</th>
              <th className="num">MVP</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              // Finding yourself among ten names shouldn't take a moment.
              <tr key={r.steam_id} className={r.steam_id === meSteamId ? "row-me" : undefined}>
                <td>
                  {/* /profiles/<steamid64> always resolves and redirects to a
                      vanity URL when the player has one, so nothing extra
                      needs storing to link here. */}
                  <a
                    className="player"
                    href={`https://steamcommunity.com/profiles/${r.steam_id}`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {/* Falls back to a blank circle rather than a broken
                        image when the profile couldn't be read. */}
                    {r.avatar_url ? (
                      <img className="player-avatar" src={r.avatar_url} alt="" width={24} height={24} loading="lazy" />
                    ) : (
                      <span className="player-avatar player-avatar-empty" aria-hidden="true" />
                    )}
                    {r.name || r.steam_id}
                  </a>
                </td>
                <td className="num">{r.kills}</td>
                <td className="num">{r.assists}</td>
                <td className="num">{r.deaths}</td>
                <td className="num">{r.adr.toFixed(1)}</td>
                <td className="num">{r.headshot_pct.toFixed(0)}%</td>
                <td className="num">{r.mvps}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
