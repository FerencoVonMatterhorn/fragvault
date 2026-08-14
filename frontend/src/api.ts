// Thin fetch wrappers around the Go backend's API. Cookies (the session)
// are sent automatically since frontend and backend share an origin in
// production (nginx serves both) and via the Vite dev proxy locally.

export interface Me {
  steam_id: string;
  persona: string;
  avatar_url: string;
}

export interface Match {
  share_code: string;
  match_id: number;
  reservation_id: number;
  tv_port: number;
  discovered_at: string;
}

/**
 * The backend couldn't be reached at all — it's down, restarting, or the
 * reverse proxy has no upstream to talk to. Distinct from an error the
 * backend itself returned, because the frontend can still render usefully:
 * this is "come back in a minute", not "the app is broken".
 */
export class BackendUnavailableError extends Error {
  constructor(message = "backend unavailable") {
    super(message);
    this.name = "BackendUnavailableError";
  }
}

async function jsonOrThrow<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(`${res.status} ${res.statusText}: ${text}`);
  }
  return res.json() as Promise<T>;
}

export async function getMe(): Promise<Me | null> {
  let res: Response;
  try {
    res = await fetch("/api/me", { credentials: "include" });
  } catch {
    // fetch only rejects on network-level failure (connection refused, DNS,
    // offline). Any HTTP status, including 502, resolves normally.
    throw new BackendUnavailableError();
  }
  if (res.status === 401) return null;
  // 5xx here is nearly always the reverse proxy reporting that the backend
  // isn't answering, rather than the backend rejecting the request.
  if (res.status >= 500) throw new BackendUnavailableError(`${res.status} ${res.statusText}`);
  return jsonOrThrow<Me>(res);
}

export async function getMatches(): Promise<Match[]> {
  const res = await fetch("/api/matches", { credentials: "include" });
  const data = await jsonOrThrow<{ matches: Match[] }>(res);
  return data.matches ?? [];
}

export async function submitOnboarding(authCode: string, startingShareCode: string): Promise<void> {
  const res = await fetch("/api/onboarding", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ auth_code: authCode, starting_share_code: startingShareCode }),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(`${res.status} ${res.statusText}: ${text}`);
  }
}

export function loginUrl(): string {
  return "/auth/steam/login";
}

/**
 * Clears the session cookie server-side. The cookie is HttpOnly, so the
 * browser can't drop it on its own — this has to be a request.
 */
export async function logout(): Promise<void> {
  const res = await fetch("/auth/logout", { method: "POST", credentials: "include" });
  if (!res.ok) {
    throw new Error(`${res.status} ${res.statusText}`);
  }
}
