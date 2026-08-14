import { useState } from "react";
import { submitOnboarding } from "./api";

/**
 * One-time connection of a Steam account's match history.
 *
 * Its own page rather than a panel on the match list: it's done once and
 * then never again, so it shouldn't occupy space on the page you visit
 * daily — but it does need to be findable afterwards, since the auth code
 * expires and has to be replaced.
 */
export default function SetupPage() {
  const [authCode, setAuthCode] = useState("");
  const [startingShareCode, setStartingShareCode] = useState("");
  const [status, setStatus] = useState<"idle" | "saving" | "saved" | "error">("idle");
  const [errorMsg, setErrorMsg] = useState("");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setStatus("saving");
    setErrorMsg("");
    try {
      await submitOnboarding(authCode, startingShareCode);
      setStatus("saved");
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : String(err));
      setStatus("error");
    }
  }

  return (
    <section className="section">
      <h1 style={{ fontSize: 32, fontWeight: 600, letterSpacing: "-0.012em", margin: "0 0 8px" }}>Setup</h1>
      <p className="muted" style={{ marginTop: 0 }}>
        Connect your CS2 match history. This is a one-time step, and it's the only part FragVault can't do for you —
        Valve requires your own authentication code to read your matches.
      </p>

      <div className="panel" style={{ marginTop: 24 }}>
        <form onSubmit={submit} className="stack" style={{ maxWidth: 420 }}>
          <label className="stack" style={{ gap: 6 }}>
            <span className="small">Game authentication code</span>
            <input
              className="field"
              placeholder="XXXXX-XXXXX-XXXXX"
              value={authCode}
              onChange={(e) => setAuthCode(e.target.value)}
            />
            <span className="small muted">
              From{" "}
              <a
                href="https://help.steampowered.com/en/wizard/HelpWithGameIssue?appid=730&issueid=128"
                target="_blank"
                rel="noreferrer"
              >
                Steam Support
              </a>
              . It's tied to your account and only readable by you.
            </span>
          </label>

          <label className="stack" style={{ gap: 6 }}>
            <span className="small">Starting sharecode</span>
            <input
              className="field"
              placeholder="CSGO-xxxxx-xxxxx-xxxxx-xxxxx-xxxxx"
              value={startingShareCode}
              onChange={(e) => setStartingShareCode(e.target.value)}
            />
            <span className="small muted">
              From CS2's match history. Use an <strong>older</strong> match — discovery walks <em>forward</em> from this
              code, so your most recent match finds nothing at all.
            </span>
          </label>

          <div>
            <button className="btn" type="submit" disabled={status === "saving"}>
              {status === "saving" ? "Saving…" : "Save"}
            </button>
          </div>

          {status === "saved" && (
            <p className="small success">Saved. Open Matches and hit Refresh to discover your games.</p>
          )}
          {status === "error" && <p className="small error">{errorMsg}</p>}
        </form>
      </div>
    </section>
  );
}
