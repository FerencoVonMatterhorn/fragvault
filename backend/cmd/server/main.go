// Command server is the FragVault Phase 1 backend: Steam OpenID login plus
// CS2 match discovery via sharecode polling. See /docs/architecture.md for
// the full design and its known limitations.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/fragvault/fragvault/backend/internal/matches"
	"github.com/fragvault/fragvault/backend/internal/session"
	"github.com/fragvault/fragvault/backend/internal/steamauth"
)

func requireEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", name)
	}
	return v
}

func main() {
	addr := envOr("LISTEN_ADDR", ":8080")
	storePath := envOr("STORE_PATH", "./fragvault-data.json")
	baseURL := requireEnv("BASE_URL") // e.g. https://fragvault.gg (or http://<vm-ip> for smoke testing)
	steamWebAPIKey := requireEnv("STEAM_WEB_API_KEY")
	sessionSecretHex := requireEnv("SESSION_SECRET") // random 32+ byte secret, hex or any string
	devInsecureCookie := os.Getenv("DEV_INSECURE_COOKIE") == "true"

	steamClient := steamauth.NewClient(steamWebAPIKey, baseURL+"/auth/steam/callback", baseURL)
	sessions := session.NewManager([]byte(sessionSecretHex), 30*24*time.Hour, !devInsecureCookie)
	store := matches.NewStore(storePath)
	steamAPI := matches.NewSteamAPIClient(steamWebAPIKey)

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

		if err := store.UpsertProfile(steamID, persona, avatar); err != nil {
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
		if err := store.SetOnboarding(claims.SteamID, body.AuthCode, body.StartingShareCode); err != nil {
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

		rec, err := store.GetPlayer(claims.SteamID)
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
			if serr := store.AppendMatches(claims.SteamID, found, newestCode); serr != nil {
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

		rec, err = store.GetPlayer(claims.SteamID)
		if err != nil {
			log.Printf("error: GetPlayer (post-poll) failed for %s: %v", claims.SteamID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"matches": rec.Matches,
		})
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
