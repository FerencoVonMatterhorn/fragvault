import type { Highlight } from "./api";

const KIND_LABELS: Record<string, string> = {
  multi_kill: "Multi-kill",
  clutch: "Clutch",
  opening_duel: "Opening duel",
  defuse: "Defuse",
};

export function formatClock(seconds: number): string {
  const total = Math.max(0, Math.round(seconds));
  const mins = Math.floor(total / 60);
  const secs = total % 60;
  return `${mins}:${secs.toString().padStart(2, "0")}`;
}

/** Shared so the match list and the detail page can't drift apart. */
export function HighlightList({ highlights }: { highlights: Highlight[] }) {
  if (highlights.length === 0) {
    return <p className="small muted">Parsed successfully, but nothing met the highlight thresholds.</p>;
  }

  return (
    <ol className="highlights">
      {highlights.map((h, i) => (
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
  );
}
