import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { getAnalysis, type Analysis, type Me, type ScoreboardRow } from "./api";
import { HighlightList } from "./Highlights";
import { mapDisplayName, mapImageUrl } from "./maps";
import { RoundTimeline } from "./RoundTimeline";

// CS2 team ids.
export const TEAM_T = 2;
export const TEAM_CT = 3;

type Tab = "scoreboard" | "highlights";

export default function MatchPage({ me }: { me: Me }) {
  const { shareCode = "" } = useParams();
  const [analysis, setAnalysis] = useState<Analysis | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [errorMsg, setErrorMsg] = useState("");
  // A view toggle, not a route: putting it in the URL would mean handling an
  // invalid tab name for no benefit.
  const [tab, setTab] = useState<Tab>("scoreboard");
  const [showEveryone, setShowEveryone] = useState(false);

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
  const mapImage = mapImageUrl(analysis?.map_name);

  // Detectors run over everyone in the demo, so an enemy's ace is in here
  // too. This is a product about *your* best plays, so yours are the default
  // and the rest are opt-in.
  const mine = analysis?.highlights.filter((h) => h.steam_id === me.steam_id) ?? [];
  const shown = showEveryone ? (analysis?.highlights ?? []) : mine;

  // Which score is yours. Sides swap at half time, so "13–11" is meaningless
  // without knowing which side you ended on — and the scoreboard row is the
  // only thing that says.
  const myRow = analysis?.scoreboard.find((r) => r.steam_id === me.steam_id);
  const myScore = myRow?.team === TEAM_T ? analysis?.team_a_score : analysis?.team_b_score;
  const theirScore = myRow?.team === TEAM_T ? analysis?.team_b_score : analysis?.team_a_score;
  const oriented = myRow !== undefined && myScore !== undefined && theirScore !== undefined;

  return (
    <>
      <p className="section" style={{ marginBottom: 8 }}>
        <Link to="/" className="small">
          ‹ All matches
        </Link>
      </p>

      <header className="match-header">
        <div className="match-title">
          {/* Decorative: the map name sits right beside it, so announcing the
              image too would only repeat it. */}
          {mapImage && <img className="map-icon" src={mapImage} alt="" loading="lazy" />}
          <div style={{ minWidth: 0 }}>
            <h1>{mapDisplayName(analysis?.map_name) || "Match"}</h1>
            <p className="mono caption muted" style={{ margin: "4px 0 0" }}>
              {shareCode}
            </p>
          </div>
        </div>

        {done && (
          <div style={{ marginTop: 18 }}>
            <p className="scoreline" style={{ margin: "0 0 4px" }}>
              {oriented ? (
                <>
                  {myScore}
                  <span className="muted">–</span>
                  {theirScore}
                </>
              ) : (
                <>
                  {analysis.team_a_score}
                  <span className="muted">–</span>
                  {analysis.team_b_score}
                </>
              )}
            </p>
            <p className="caption muted" style={{ margin: "0 0 12px" }}>
              {oriented
                ? myScore === theirScore
                  ? "Draw"
                  : myScore! > theirScore!
                    ? "You won"
                    : "You lost"
                : "T–CT at the end of the match"}
            </p>
            {/* Marks your moments, not everyone's — the strip should answer
                "where were my good rounds". */}
            <RoundTimeline rounds={analysis.rounds} highlights={mine} />
          </div>
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
              Highlights <span className="muted">{mine.length}</span>
            </button>
          </nav>

          {tab === "scoreboard" ? (
            <Scoreboard rows={analysis.scoreboard} meSteamId={me.steam_id} />
          ) : (
            <>
              <div className="row" style={{ justifyContent: "space-between", marginBottom: 12 }}>
                <span className="caption muted">
                  {showEveryone ? `${analysis.highlights.length} in this match` : `${mine.length} of yours`}
                </span>
                <button className="linklike caption" onClick={() => setShowEveryone((v) => !v)}>
                  {showEveryone ? "Show only mine" : "Show everyone's"}
                </button>
              </div>
              <HighlightList highlights={shown} showPlayer={showEveryone} scoreboard={analysis.scoreboard} />
            </>
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
      {ct.length > 0 && <TeamTable title="Counter-Terrorists" side="ct" rows={ct} meSteamId={meSteamId} />}
      {t.length > 0 && <TeamTable title="Terrorists" side="t" rows={t} meSteamId={meSteamId} />}
      {other.length > 0 && <TeamTable title="Unassigned" side="none" rows={other} meSteamId={meSteamId} />}
    </>
  );
}

function TeamTable({
  title,
  side,
  rows,
  meSteamId,
}: {
  title: string;
  side: "ct" | "t" | "none";
  rows: ScoreboardRow[];
  meSteamId: string;
}) {
  return (
    // The side colour is doing real work here: two tables of ten names are
    // otherwise told apart only by reading their headings.
    <section className={side === "none" ? "team" : `team team-${side}`}>
      <h2 style={{ marginBottom: 10 }}>{title}</h2>
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
