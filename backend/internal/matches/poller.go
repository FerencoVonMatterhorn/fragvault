package matches

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const nextShareCodeEndpoint = "https://api.steampowered.com/ICSGOPlayers_730/GetNextMatchSharingCode/v1/"

// SteamAPIClient walks a player's match history forward using Valve's
// official (if minimal) mechanism: given a game auth code and the last known
// sharecode, repeatedly ask for the next one until there isn't one.
type SteamAPIClient struct {
	httpClient *http.Client
	apiKey     string // Steam Web API key, server-side secret
}

func NewSteamAPIClient(apiKey string) *SteamAPIClient {
	return &SteamAPIClient{httpClient: &http.Client{}, apiKey: apiKey}
}

// ErrNoNewerMatch is returned by NextShareCode when Steam reports there is no
// match newer than knownCode (HTTP 202 from the API).
var ErrNoNewerMatch = fmt.Errorf("no newer match available")

type nextShareCodeResponse struct {
	Result struct {
		NextCode string `json:"nextcode"`
	} `json:"result"`
}

// NextShareCode calls GetNextMatchSharingCode. steamID is the player's
// steamid64, authCode is their one-time "game authentication code" from
// help.steampowered.com, and knownCode is the most recent sharecode already
// known for this player.
func (c *SteamAPIClient) NextShareCode(steamID, authCode, knownCode string) (string, error) {
	q := url.Values{
		"key":        {c.apiKey},
		"steamid":    {steamID},
		"steamidkey": {authCode},
		"knowncode":  {knownCode},
	}
	resp, err := c.httpClient.Get(nextShareCodeEndpoint + "?" + q.Encode())
	if err != nil {
		return "", fmt.Errorf("calling GetNextMatchSharingCode: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed nextShareCodeResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return "", fmt.Errorf("decoding GetNextMatchSharingCode response: %w", err)
		}
		if parsed.Result.NextCode == "" {
			return "", fmt.Errorf("GetNextMatchSharingCode returned 200 with no nextcode")
		}
		return parsed.Result.NextCode, nil
	case http.StatusAccepted, http.StatusNotFound:
		// 202 = no newer match. Some deployments have also been observed
		// returning 4xx for the "nothing new" case; treat not-found the same
		// way defensively rather than surfacing it as a hard error.
		return "", ErrNoNewerMatch
	default:
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GetNextMatchSharingCode returned %d: %s", resp.StatusCode, string(body))
	}
}

// DiscoverNewMatches walks forward from knownCode until ErrNoNewerMatch,
// decoding each new sharecode along the way. It stops and returns whatever
// it found so far if a decode error occurs partway through, so one bad
// sharecode doesn't lose matches discovered before it.
func (c *SteamAPIClient) DiscoverNewMatches(steamID, authCode, knownCode string) (newest string, found []Match, err error) {
	current := knownCode
	for {
		next, nerr := c.NextShareCode(steamID, authCode, current)
		if nerr == ErrNoNewerMatch {
			return current, found, nil
		}
		if nerr != nil {
			return current, found, nerr
		}

		decoded, derr := DecodeShareCode(next)
		if derr != nil {
			return current, found, fmt.Errorf("decoding newly discovered sharecode %q: %w", next, derr)
		}

		found = append(found, Match{
			ShareCode:     next,
			MatchID:       decoded.MatchID,
			ReservationID: decoded.ReservationID,
			TVPort:        decoded.TVPort,
		})
		current = next
	}
}
