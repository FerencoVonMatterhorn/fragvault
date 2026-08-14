// Package steamauth implements "Sign in through Steam" using Steam's
// OpenID 2.0 endpoint (Steam has no OAuth) and looks up the logged-in
// player's public profile via the Steam Web API. Implemented against the
// standard library only — see /docs/adr-001-no-deps-phase1.md.
package steamauth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	steamOpenIDEndpoint = "https://steamcommunity.com/openid/login"
	openIDNamespace     = "http://specs.openid.net/auth/2.0"
	claimedIDPattern    = `^https://steamcommunity\.com/openid/id/(\d+)$`
)

var claimedIDRe = regexp.MustCompile(claimedIDPattern)

// Client drives the Steam OpenID login flow and Steam Web API lookups.
type Client struct {
	httpClient *http.Client
	webAPIKey  string // Steam Web API key, server-side secret
	returnURL  string // e.g. https://fragvault.gg/auth/steam/callback
	realm      string // e.g. https://fragvault.gg
}

func NewClient(webAPIKey, returnURL, realm string) *Client {
	return &Client{
		httpClient: &http.Client{},
		webAPIKey:  webAPIKey,
		returnURL:  returnURL,
		realm:      realm,
	}
}

// LoginURL builds the URL to redirect the browser to in order to start the
// Steam OpenID flow. GET /auth/steam/login should 302 here.
func (c *Client) LoginURL() string {
	q := url.Values{
		"openid.ns":         {openIDNamespace},
		"openid.mode":       {"checkid_setup"},
		"openid.return_to":  {c.returnURL},
		"openid.realm":      {c.realm},
		"openid.identity":   {"http://specs.openid.net/auth/2.0/identifier_select"},
		"openid.claimed_id": {"http://specs.openid.net/auth/2.0/identifier_select"},
	}
	return steamOpenIDEndpoint + "?" + q.Encode()
}

// VerifyCallback validates the OpenID assertion Steam redirected back with,
// using OpenID's "stateless" (direct) verification: we re-post the exact
// params back to Steam with openid.mode=check_authentication and trust the
// response only if Steam echoes "is_valid:true". Returns the player's
// steamid64 on success.
func (c *Client) VerifyCallback(query url.Values) (steamID64 string, err error) {
	claimedID := query.Get("openid.claimed_id")
	m := claimedIDRe.FindStringSubmatch(claimedID)
	if m == nil {
		return "", fmt.Errorf("unexpected or missing openid.claimed_id: %q", claimedID)
	}
	steamID64 = m[1]

	verify := url.Values{}
	for k, v := range query {
		verify[k] = v
	}
	verify.Set("openid.mode", "check_authentication")

	req, err := http.NewRequest(http.MethodPost, steamOpenIDEndpoint, strings.NewReader(verify.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("posting verification to steam: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if !strings.Contains(string(body), "is_valid:true") {
		return "", fmt.Errorf("steam rejected the openid assertion")
	}
	return steamID64, nil
}

// PlayerSummary is the subset of Steam's GetPlayerSummaries response we use.
type PlayerSummary struct {
	SteamID     string `json:"steamid"`
	PersonaName string `json:"personaname"`
	AvatarFull  string `json:"avatarfull"`
	ProfileURL  string `json:"profileurl"`
}

type getPlayerSummariesResponse struct {
	Response struct {
		Players []PlayerSummary `json:"players"`
	} `json:"response"`
}

// GetPlayerSummaries fetches public profiles for many steamids in one call.
//
// Used to put faces on the scoreboard. Steam accepts up to 100 ids per
// request, which is well past the ten in a match, so this is one round trip
// per analysis rather than one per player.
//
// Returns what it found: a player whose profile is unavailable is simply
// absent from the map rather than an error, since a missing avatar is not a
// reason to fail an analysis.
func (c *Client) GetPlayerSummaries(steamIDs []string) (map[string]PlayerSummary, error) {
	out := map[string]PlayerSummary{}
	if len(steamIDs) == 0 {
		return out, nil
	}

	const batchSize = 100
	for start := 0; start < len(steamIDs); start += batchSize {
		end := min(start+batchSize, len(steamIDs))

		q := url.Values{
			"key":      {c.webAPIKey},
			"steamids": {strings.Join(steamIDs[start:end], ",")},
		}
		endpoint := "https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/?" + q.Encode()

		resp, err := c.httpClient.Get(endpoint)
		if err != nil {
			return out, fmt.Errorf("calling GetPlayerSummaries: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return out, fmt.Errorf("GetPlayerSummaries returned %d: %s", resp.StatusCode, string(body))
		}

		var parsed getPlayerSummariesResponse
		err = json.NewDecoder(resp.Body).Decode(&parsed)
		resp.Body.Close()
		if err != nil {
			return out, fmt.Errorf("decoding GetPlayerSummaries response: %w", err)
		}

		for _, p := range parsed.Response.Players {
			out[p.SteamID] = p
		}
	}

	return out, nil
}

// GetPlayerSummary fetches the public profile (display name, avatar) for a
// steamid64 via the Steam Web API.
func (c *Client) GetPlayerSummary(steamID64 string) (*PlayerSummary, error) {
	q := url.Values{
		"key":      {c.webAPIKey},
		"steamids": {steamID64},
	}
	endpoint := "https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/?" + q.Encode()

	resp, err := c.httpClient.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("calling GetPlayerSummaries: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GetPlayerSummaries returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed getPlayerSummariesResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding GetPlayerSummaries response: %w", err)
	}
	if len(parsed.Response.Players) == 0 {
		return nil, fmt.Errorf("no player found for steamid %s", steamID64)
	}
	return &parsed.Response.Players[0], nil
}
