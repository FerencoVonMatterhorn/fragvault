import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { getBestHighlights, type BestHighlight } from "./api";
import { formatClock } from "./Highlights";
import { mapDisplayName, mapImageUrl } from "./maps";

const KIND_LABELS: Record<string, string> = {
  multi_kill: "Multi-kill",
  clutch: "Clutch",
  opening_duel: "Opening",
  defuse: "Defuse",
};

function detail(h: BestHighlight): string {
  const meta = h.metadata ?? {};
  if (h.kind === "multi_kill") {
    const kills = Number(meta.kills ?? 0);
    return kills >= 5 ? "Ace" : kills > 0 ? `${kills}K` : "";
  }
  if (h.kind === "clutch") {
    const enemies = Number(meta.enemies_alive ?? 0);
    return enemies > 0 ? `1v${enemies}` : "";
  }
  if (h.kind === "defuse") {
    const left = Number(meta.time_left ?? 0);
    return left > 0 ? `${left.toFixed(1)}s left` : "";
  }
  return typeof meta.weapon === "string" ? meta.weapon : "";
}

/**
 * Your best moments across every match, best first.
 *
 * This is what the product actually promises, so it gets a page rather than
 * living inside individual matches.
 */
export default function HighlightsPage() {
  const [highlights, setHighlights] = useState<BestHighlight[] | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [errorMsg, setErrorMsg] = useState("");

  useEffect(() => {
    let cancelled = false;
    getBestHighlights()
      .then((h) => {
        if (cancelled) return;
        setHighlights(h);
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
  }, []);

  return (
    <section className="section">
      <div className="section-head">
        <h1>Highlights</h1>
      </div>

      {state === "loading" && <p className="small muted">Loading…</p>}
      {state === "error" && <p className="small error">{errorMsg}</p>}

      {state === "ready" && highlights && highlights.length === 0 && (
        <p className="small muted">
          Nothing yet. Analyse a match from <Link to="/matches">Match history</Link> and your best moments land here.
        </p>
      )}

      {state === "ready" && highlights && highlights.length > 0 && (
        <ul className="list-reset">
          {highlights.map((h, i) => {
            const icon = mapImageUrl(h.map_name);
            return (
              <li className="card" key={`${h.share_code}-${h.kind}-${h.round}-${i}`}>
                <Link
                  to={`/match/${encodeURIComponent(h.share_code)}`}
                  className="card-head"
                  style={{ color: "inherit", textDecoration: "none" }}
                >
                  <div className="card-head-main">
                    {icon && <img className="map-thumb" src={icon} alt="" loading="lazy" />}
                    <div style={{ minWidth: 0 }}>
                      <h2 style={{ fontSize: "var(--t-small)" }}>
                        {KIND_LABELS[h.kind] ?? h.kind}
                        {detail(h) ? <span className="muted"> · {detail(h)}</span> : null}
                      </h2>
                      <div className="caption muted">
                        {mapDisplayName(h.map_name) || "Unknown map"} · round {h.round}
                      </div>
                    </div>
                  </div>
                  <span className="mono caption muted">
                    {formatClock(h.start_s)}–{formatClock(h.end_s)}
                  </span>
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
