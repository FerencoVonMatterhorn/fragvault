import type { Highlight, RoundResult } from "./api";

// CS2 team ids.
const TEAM_T = 2;
const TEAM_CT = 3;

// Sides swap after twelve rounds in the current MR12 format, and the strip
// should show that break rather than running as one undifferentiated bar.
const HALF_AT = 12;

/**
 * The round strip from the in-game scoreboard: one tick per round, coloured
 * by the side that won, notched where a highlight happened.
 *
 * It is the one piece of this interface meant to be memorable, and it earns
 * that by being informative — how the match went, and where the good moments
 * were, without reading a single number.
 */
export function RoundTimeline({
  rounds,
  highlights = [],
  small = false,
}: {
  rounds: RoundResult[];
  highlights?: Highlight[];
  small?: boolean;
}) {
  if (rounds.length === 0) return null;

  const marked = new Set(highlights.map((h) => h.round));

  return (
    <div
      className={small ? "timeline timeline-small" : "timeline"}
      role="img"
      aria-label={`Round history: ${rounds.length} rounds${
        marked.size > 0 ? `, highlights in ${marked.size} of them` : ""
      }`}
    >
      {rounds.map((r) => {
        const classes = ["tick"];
        if (r.winner === TEAM_CT) classes.push("tick-ct");
        else if (r.winner === TEAM_T) classes.push("tick-t");
        if (!small && marked.has(r.number)) classes.push("tick-marked");
        if (r.number === HALF_AT) classes.push("tick-half");

        return (
          <span
            key={r.number}
            className={classes.join(" ")}
            // Hover detail without a tooltip library, and it survives being
            // read by anything that surfaces titles.
            title={`Round ${r.number}: ${
              r.winner === TEAM_CT ? "CT" : r.winner === TEAM_T ? "T" : "unknown"
            }${marked.has(r.number) ? " · highlight" : ""}`}
          />
        );
      })}
    </div>
  );
}
