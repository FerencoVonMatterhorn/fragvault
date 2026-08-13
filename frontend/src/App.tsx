import { useEffect, useState } from "react";
import { getMe, getMatches, submitOnboarding, loginUrl, type Me, type Match } from "./api";

type LoadState = "loading" | "ready" | "error";

export default function App() {
  const [me, setMe] = useState<Me | null>(null);
  const [meState, setMeState] = useState<LoadState>("loading");

  useEffect(() => {
    getMe()
      .then((m) => {
        setMe(m);
        setMeState("ready");
      })
      .catch(() => setMeState("error"));
  }, []);

  if (meState === "loading") return <Centered>Loading…</Centered>;
  if (meState === "error") return <Centered>Something went wrong talking to the backend.</Centered>;

  return (
    <div style={{ maxWidth: 640, margin: "4rem auto", fontFamily: "system-ui, sans-serif" }}>
      <h1>FragVault</h1>
      {me ? <LoggedIn me={me} /> : <LoggedOut />}
    </div>
  );
}

function LoggedOut() {
  return (
    <div>
      <p>Sign in with Steam to see your recent CS2 matches.</p>
      <a href={loginUrl()}>
        <button style={{ padding: "0.6rem 1.2rem", fontSize: "1rem" }}>Log in with Steam</button>
      </a>
    </div>
  );
}

function LoggedIn({ me }: { me: Me }) {
  return (
    <div>
      <p>
        Signed in as <strong>{me.persona}</strong>
        {me.avatar_url && (
          <img src={me.avatar_url} alt="" width={32} height={32} style={{ verticalAlign: "middle", marginLeft: 8 }} />
        )}
      </p>
      <Onboarding />
      <MatchList />
    </div>
  );
}

function Onboarding() {
  const [authCode, setAuthCode] = useState("");
  const [startingShareCode, setStartingShareCode] = useState("");
  const [status, setStatus] = useState<"idle" | "saving" | "saved" | "error">("idle");
  const [errorMsg, setErrorMsg] = useState("");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setStatus("saving");
    try {
      await submitOnboarding(authCode, startingShareCode);
      setStatus("saved");
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : String(err));
      setStatus("error");
    }
  }

  return (
    <details style={{ margin: "1.5rem 0", border: "1px solid #ccc", borderRadius: 8, padding: "0.75rem 1rem" }}>
      <summary style={{ cursor: "pointer", fontWeight: 600 }}>One-time setup: connect your match history</summary>
      <p style={{ fontSize: "0.9rem", color: "#444" }}>
        Get your game authentication code from{" "}
        <a href="https://help.steampowered.com/en/wizard/HelpWithGameIssue?appid=730&issueid=128" target="_blank" rel="noreferrer">
          Steam Support
        </a>{" "}
        and a starting sharecode from CS2's in-game match history settings, then paste both below. This only needs to be
        done once.
      </p>
      <form onSubmit={submit} style={{ display: "grid", gap: "0.5rem", maxWidth: 360 }}>
        <input placeholder="Game auth code" value={authCode} onChange={(e) => setAuthCode(e.target.value)} />
        <input
          placeholder="Starting sharecode (CSGO-...)"
          value={startingShareCode}
          onChange={(e) => setStartingShareCode(e.target.value)}
        />
        <button type="submit" disabled={status === "saving"}>
          Save
        </button>
        {status === "saved" && <p style={{ color: "green" }}>Saved — refresh matches below.</p>}
        {status === "error" && <p style={{ color: "crimson" }}>{errorMsg}</p>}
      </form>
    </details>
  );
}

function MatchList() {
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
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2>Recent matches</h2>
        <button onClick={load} disabled={state === "loading"}>
          Refresh
        </button>
      </div>
      {state === "loading" && <p>Checking for matches…</p>}
      {state === "error" && <p style={{ color: "crimson" }}>{errorMsg}</p>}
      {state === "ready" && matches && matches.length === 0 && <p>No matches discovered yet.</p>}
      {state === "ready" && matches && matches.length > 0 && (
        <ul>
          {[...matches].reverse().map((m) => (
            <li key={m.share_code}>
              <code>{m.share_code}</code> — discovered {new Date(m.discovered_at).toLocaleString()}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return <div style={{ textAlign: "center", marginTop: "4rem", fontFamily: "system-ui, sans-serif" }}>{children}</div>;
}
