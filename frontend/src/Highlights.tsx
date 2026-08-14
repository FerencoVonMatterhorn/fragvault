import type { Highlight } from "./api";

// Named the way players name them, not the way the parser stores them.
const KIND_LABELS: Record<string, string> = {
  multi_kill: "Multi-kill",
  clutch: "Clutch",
  opening_duel: "Opening",
  defuse: "Defuse",
};

export function formatClock(seconds: number): string {
  const total = Math.max(0, Math.round(seconds));
  const mins = Math.floor(total / 60);
  const secs = total % 60;
  return `${mins}:${secs.toString().padStart(2, "0")}`;
}

/** A short description of what made this a highlight, from its metadata. */
function detail(h: Highlight): string {
  const meta = h.metadata ?? {};
  if (h.kind === "multi_kill") {
    const kills = Number(meta.kills ?? 0);
    if (kills >= 5) return "Ace";
    return kills > 0 ? `${kills}K` : "";
  }
  if (h.kind === "clutch") {
    const enemies = Number(meta.enemies_alive ?? 0);
    return enemies > 0 ? `1v${enemies}` : "";
  }
  if (h.kind === "defuse") {
    const left = Number(meta.time_left ?? 0);
    return left > 0 ? `${left.toFixed(1)}s left` : "";
  }
  if (h.kind === "opening_duel" && typeof meta.weapon === "string") {
    return meta.weapon;
  }
  return "";
}

/** Shared so the match list and the detail page can't drift apart. */
export function HighlightList({ highlights }: { highlights: Highlight[] }) {
  if (highlights.length === 0) {
    return <p className="small muted">Parsed cleanly, but nothing crossed the highlight thresholds.</p>;
  }

  return (
    <ol className="highlights">
      {highlights.map((h, i) => (
        <li className="highlight" key={`${h.kind}-${h.round}-${h.start_s}-${i}`}>
          <span className="highlight-kind">{KIND_LABELS[h.kind] ?? h.kind}</span>
          <span className="highlight-round">R{h.round}</span>
          <span className="small muted">{detail(h)}</span>
          <span className="mono caption muted">
            {formatClock(h.start_s)}–{formatClock(h.end_s)}
          </span>
        </li>
      ))}
    </ol>
  );
}
