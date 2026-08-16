// Command server is the FragVault Phase 1 backend: Steam OpenID login plus
// CS2 match discovery via sharecode polling. See /docs/architecture.md for
// the full design and its known limitations.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/fragvault/fragvault/backend/internal/db"
	"github.com/fragvault/fragvault/backend/internal/demos"
	"github.com/fragvault/fragvault/backend/internal/matches"
	"github.com/fragvault/fragvault/backend/internal/session"
	"github.com/fragvault/fragvault/backend/internal/steamauth"
)

// steamAvatars adapts the Steam client to what the analysis worker needs,
// which is only "these ids, please" — the worker has no business knowing
// about profile summaries.
type steamAvatars struct {
	client *steamauth.Client
}

func (s steamAvatars) AvatarsFor(steamIDs []string) (map[string]string, error) {
	summaries, err := s.client.GetPlayerSummaries(steamIDs)
	out := make(map[string]string, len(summaries))
	for id, summary := range summaries {
		if summary.AvatarFull != "" {
			out[id] = summary.AvatarFull
		}
	}
	// Partial results are returned alongside the error on purpose: some
	// avatars beat none.
	return out, err
}

func requireEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", name)
	}
	return v
}

func main() {
	addr := envOr("LISTEN_ADDR", ":8080")
	// Only still read to migrate it into Postgres on first boot; nothing
	// writes to this file any more.
	legacyStorePath := envOr("STORE_PATH", "./fragvault-data.json")
	databaseURL := requireEnv("DATABASE_URL")
	baseURL := requireEnv("BASE_URL") // e.g. https://fragvault.gg (or http://<vm-ip> for smoke testing)
	steamWebAPIKey := requireEnv("STEAM_WEB_API_KEY")
	sessionSecretHex := requireEnv("SESSION_SECRET") // random 32+ byte secret, hex or any string
	devInsecureCookie := os.Getenv("DEV_INSECURE_COOKIE") == "true"

	ctx := context.Background()

	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("running migrations: %v", err)
	}

	imported, err := db.ImportLegacyJSON(ctx, pool, legacyStorePath)
	if err != nil {
		log.Fatalf("importing legacy store: %v", err)
	}
	if imported > 0 {
		log.Printf("imported %d player(s) from the legacy JSON store at %s", imported, legacyStorePath)
	}

	steamClient := steamauth.NewClient(steamWebAPIKey, baseURL+"/auth/steam/callback", baseURL)
	sessions := session.NewManager([]byte(sessionSecretHex), 30*24*time.Hour, !devInsecureCookie)
	store := db.NewStore(pool)
	steamAPI := matches.NewSteamAPIClient(steamWebAPIKey)

	// Optional: without it, analysis still works from an explicitly supplied
	// demo URL, so the app degrades to the manual path rather than breaking.
	var gcResolver *demos.GCResolver
	if gcURL := os.Getenv("GC_SIDECAR_URL"); gcURL != "" {
		gcResolver = demos.NewGCResolver(gcURL)
		log.Printf("game coordinator sidecar configured at %s", gcURL)
	} else {
		log.Printf("GC_SIDECAR_URL not set — demo URLs must be supplied manually")
	}

	// Optional: without it demos are parsed and discarded exactly as before,
	// but the matches processed meanwhile can never have clips rendered from
	// them — Valve expires the demo URL and there is no second copy. Worth a
	// loud line in the log either way.
	var demoStore demos.DemoStore
	if sasURL := os.Getenv("DEMO_BLOB_SAS_URL"); sasURL != "" {
		bs, err := demos.NewBlobDemoStore(sasURL)
		if err != nil {
			// Configured but unusable is a typo, not a degraded mode. Failing
			// here is how it gets noticed at deploy time rather than a month
			// later when someone tries to render something.
			log.Fatalf("DEMO_BLOB_SAS_URL is set but unusable: %v", err)
		}
		demoStore = bs
		log.Printf("retaining parsed demos to blob storage")
	} else {
		log.Printf("DEMO_BLOB_SAS_URL not set — demos are discarded after parsing and matches analysed now will not be renderable later")
	}

	// Anything still marked running belongs to a previous process, since the
	// worker is single and in-process. Left alone they would never be picked
	// up again.
	if requeued, rerr := store.RequeueOrphaned(ctx); rerr != nil {
		log.Printf("warning: could not requeue orphaned analyses: %v", rerr)
	} else if requeued > 0 {
		log.Printf("requeued %d analysis job(s) interrupted by a restart", requeued)
	}

	// One worker, deliberately. Parsing is minutes of sustained CPU on a
	// burstable VM; running analyses concurrently would throttle the web
	// server along with them.
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	go demos.NewWorker(store, steamAvatars{steamClient}, demoStore, envOr("DEMO_TMP_DIR", os.TempDir())).Run(workerCtx)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /auth/steam/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, steamClient.LoginURL(), http.StatusFound)
	})

	mux.HandleFunc("GET /auth/steam/callback", func(w http.ResponseWriter, r *http.Request) {
		steamID, err := steamClient.VerifyCallback(r.URL.Query())
		if err != nil {
			log.Printf("steam openid verification failed: %v", err)
			http.Error(w, "steam login failed", http.StatusUnauthorized)
			return
		}

		summary, err := steamClient.GetPlayerSummary(steamID)
		persona, avatar := steamID, ""
		if err != nil {
			// Non-fatal: we still have a verified steamid even if the
			// profile lookup fails (e.g. transient Steam API issue).
			log.Printf("warning: GetPlayerSummary failed for %s: %v", steamID, err)
		} else {
			persona, avatar = summary.PersonaName, summary.AvatarFull
		}

		if err := store.UpsertProfile(r.Context(), steamID, persona, avatar); err != nil {
			log.Printf("error: UpsertProfile failed for %s: %v", steamID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := sessions.Issue(w, steamID, persona, avatar); err != nil {
			log.Printf("error: issuing session failed for %s: %v", steamID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/", http.StatusFound)
	})

	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		sessions.Clear(w)
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		claims, err := sessions.Verify(r)
		if err != nil {
			http.Error(w, "not logged in", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"steam_id":   claims.SteamID,
			"persona":    claims.Persona,
			"avatar_url": claims.AvatarURL,
		})
	})

	// POST /api/onboarding — one-time setup where the logged-in user pastes
	// their Steam "game authentication code" (from help.steampowered.com)
	// and a starting sharecode (from CS2's in-game match history settings).
	// See /docs/architecture.md for why this manual step is unavoidable.
	mux.HandleFunc("POST /api/onboarding", func(w http.ResponseWriter, r *http.Request) {
		claims, err := sessions.Verify(r)
		if err != nil {
			http.Error(w, "not logged in", http.StatusUnauthorized)
			return
		}
		var body struct {
			AuthCode          string `json:"auth_code"`
			StartingShareCode string `json:"starting_share_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.AuthCode == "" || body.StartingShareCode == "" {
			http.Error(w, "auth_code and starting_share_code are required", http.StatusBadRequest)
			return
		}
		if _, err := matches.DecodeShareCode(body.StartingShareCode); err != nil {
			http.Error(w, "starting_share_code doesn't look like a valid sharecode: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := store.SetOnboarding(r.Context(), claims.SteamID, body.AuthCode, body.StartingShareCode); err != nil {
			log.Printf("error: SetOnboarding failed for %s: %v", claims.SteamID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// GET /api/matches — polls for any new matches since the last check,
	// then returns the full known match list for the logged-in user.
	mux.HandleFunc("GET /api/matches", func(w http.ResponseWriter, r *http.Request) {
		claims, err := sessions.Verify(r)
		if err != nil {
			http.Error(w, "not logged in", http.StatusUnauthorized)
			return
		}

		rec, err := store.GetPlayer(r.Context(), claims.SteamID)
		if err != nil {
			log.Printf("error: GetPlayer failed for %s: %v", claims.SteamID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if rec == nil || rec.AuthCode == "" {
			http.Error(w, "onboarding not completed yet — POST /api/onboarding first", http.StatusPreconditionRequired)
			return
		}

		newestCode, found, derr := steamAPI.DiscoverNewMatches(claims.SteamID, rec.AuthCode, rec.LatestKnownCode)
		if len(found) > 0 {
			if serr := store.AppendMatches(r.Context(), claims.SteamID, found, newestCode); serr != nil {
				log.Printf("error: AppendMatches failed for %s: %v", claims.SteamID, serr)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		}
		if derr != nil {
			// We still return whatever matches we already have on record —
			// a transient Steam API hiccup shouldn't blank the user's list.
			log.Printf("warning: DiscoverNewMatches partial failure for %s: %v", claims.SteamID, derr)
		}

		rec, err = store.GetPlayer(r.Context(), claims.SteamID)
		if err != nil {
			log.Printf("error: GetPlayer (post-poll) failed for %s: %v", claims.SteamID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"matches": rec.Matches,
		})
	})

	// POST /api/matches/{sharecode}/analyze — queue a demo for analysis.
	// The demo URL is supplied by the caller for now; the Game Coordinator
	// sidecar will resolve it automatically later.
	mux.HandleFunc("POST /api/matches/{sharecode}/analyze", func(w http.ResponseWriter, r *http.Request) {
		claims, err := sessions.Verify(r)
		if err != nil {
			http.Error(w, "not logged in", http.StatusUnauthorized)
			return
		}
		shareCode := r.PathValue("sharecode")

		// Without this check a user could queue work against anyone's
		// sharecode, which is both a privacy leak and free CPU for them.
		owned, err := store.MatchBelongsTo(r.Context(), shareCode, claims.SteamID)
		if err != nil {
			log.Printf("error: MatchBelongsTo failed: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !owned {
			http.Error(w, "no such match for this account", http.StatusNotFound)
			return
		}

		// An explicit demo_url is optional and wins when present, which keeps
		// a manual escape hatch for demos the GC won't hand over (FACEIT,
		// a friend's file, anything already expired on Valve's side).
		var body struct {
			DemoURL string `json:"demo_url"`
		}
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
		}

		if body.DemoURL == "" {
			if gcResolver == nil {
				http.Error(w, "demo_url is required: automatic lookup is not configured", http.StatusBadRequest)
				return
			}
			resolved, rerr := gcResolver.Resolve(r.Context(), shareCode)
			if rerr != nil {
				log.Printf("warning: resolving demo URL for %s: %v", shareCode, rerr)
				if errors.Is(rerr, demos.ErrNoDemoAvailable) {
					// Expected for older matches: Valve keeps demos for a
					// limited window.
					http.Error(w, rerr.Error(), http.StatusNotFound)
					return
				}
				http.Error(w, "could not look up the demo right now", http.StatusServiceUnavailable)
				return
			}
			body.DemoURL = resolved
		}

		// Queued is false when an analysis already exists and isn't failed —
		// asking again is a no-op by design.
		queued, err := store.Enqueue(r.Context(), shareCode, body.DemoURL)
		if err != nil {
			log.Printf("error: Enqueue failed for %s: %v", shareCode, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		analysis, err := store.GetAnalysis(r.Context(), shareCode)
		if err != nil {
			log.Printf("error: GetAnalysis failed for %s: %v", shareCode, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// 202 when work was actually queued (new or retried), 200 when the
		// caller just gets the existing state back.
		status := http.StatusOK
		if queued {
			status = http.StatusAccepted
		}
		writeJSON(w, status, analysis)
	})

	// GET /api/highlights — the player's own best moments across every
	// analysed match. The product's actual promise, in one list.
	mux.HandleFunc("GET /api/highlights", func(w http.ResponseWriter, r *http.Request) {
		claims, err := sessions.Verify(r)
		if err != nil {
			http.Error(w, "not logged in", http.StatusUnauthorized)
			return
		}

		best, err := store.BestHighlights(r.Context(), claims.SteamID, 50)
		if err != nil {
			log.Printf("error: BestHighlights failed for %s: %v", claims.SteamID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"highlights": best})
	})

	// GET /api/matches/{sharecode}/analysis — status and highlights.
	mux.HandleFunc("GET /api/matches/{sharecode}/analysis", func(w http.ResponseWriter, r *http.Request) {
		claims, err := sessions.Verify(r)
		if err != nil {
			http.Error(w, "not logged in", http.StatusUnauthorized)
			return
		}
		shareCode := r.PathValue("sharecode")

		owned, err := store.MatchBelongsTo(r.Context(), shareCode, claims.SteamID)
		if err != nil {
			log.Printf("error: MatchBelongsTo failed: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !owned {
			http.Error(w, "no such match for this account", http.StatusNotFound)
			return
		}

		analysis, err := store.GetAnalysis(r.Context(), shareCode)
		if err != nil {
			log.Printf("error: GetAnalysis failed for %s: %v", shareCode, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if analysis == nil {
			// Never asked for. Not an error — the UI shows this as
			// "not analysed yet".
			writeJSON(w, http.StatusOK, map[string]any{"share_code": shareCode, "status": "none", "highlights": []any{}})
			return
		}
		writeJSON(w, http.StatusOK, analysis)
	})

	log.Printf("fragvault backend listening on %s (base_url=%s)", addr, baseURL)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
