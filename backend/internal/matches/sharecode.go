// Package matches implements CS2 match discovery via Valve's official
// sharecode-walking mechanism (see /docs/architecture.md for the full
// explanation and its known limitations). Standard library only.
package matches

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// dictionary is the base57 alphabet Valve's client uses to render sharecodes
// as text (deliberately excludes visually ambiguous characters like 0/O, 1/I/l).
const dictionary = "ABCDEFGHJKLMNOPQRSTUVWXYZabcdefhijkmnopqrstuvwxyz23456789"

var dictLen = big.NewInt(int64(len(dictionary)))

var shareCodeRe = regexp.MustCompile(`^CSGO(-[A-Za-z0-9]{5}){5}$`)

// DecodedShareCode is the identifying info encoded inside a match sharecode.
// Field names/layout confirmed against akiver/csgo-sharecode (a widely used
// community reference implementation; Valve does not document this format
// itself): 8 bytes matchId, 8 bytes reservationId, 2 bytes tvPort, each
// stored little-endian within the decoded 18-byte buffer.
// It does NOT include human-readable details like map or score — see
// /docs/architecture.md ("known limitation") for why, and what it would take
// to get those.
type DecodedShareCode struct {
	MatchID       uint64
	ReservationID uint64 // sometimes called "outcomeId" in other ports
	TVPort        uint16
}

// DecodeShareCode decodes a Valve match sharecode like
// "CSGO-AbC12-dEf34-Gh567-iJk89-LmN01" into its underlying identifiers.
//
// NOTE: this reimplements a community-reverse-engineered format (Valve does
// not document it), cross-checked against akiver/csgo-sharecode. Still worth
// validating against a real sharecode from a live account early (see plan's
// verification section) before relying on it further.
func DecodeShareCode(shareCode string) (DecodedShareCode, error) {
	if !shareCodeRe.MatchString(shareCode) {
		return DecodedShareCode{}, fmt.Errorf("not a well-formed sharecode: %q", shareCode)
	}

	stripped := strings.ReplaceAll(strings.TrimPrefix(shareCode, "CSGO"), "-", "")
	if len(stripped) != 25 {
		return DecodedShareCode{}, fmt.Errorf("unexpected sharecode length after stripping: %q", shareCode)
	}

	// Reverse the character order, then treat as a base57 big-endian number.
	runes := []rune(stripped)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}

	acc := new(big.Int)
	for _, r := range runes {
		idx := strings.IndexRune(dictionary, r)
		if idx < 0 {
			return DecodedShareCode{}, fmt.Errorf("character %q not in sharecode dictionary", r)
		}
		acc.Mul(acc, dictLen)
		acc.Add(acc, big.NewInt(int64(idx)))
	}

	// acc should hold an 18-byte value. A malformed/mistyped sharecode can
	// decode to a larger number (57^25 slightly exceeds 2^144); treat that as
	// an invalid sharecode rather than panicking on a fixed-size buffer.
	raw := acc.Bytes() // big-endian, no leading zero bytes
	if len(raw) > 18 {
		return DecodedShareCode{}, fmt.Errorf("sharecode %q decoded to an oversized value — not a valid sharecode", shareCode)
	}
	be := make([]byte, 18)
	copy(be[18-len(raw):], raw)

	le := make([]byte, 18)
	for i, b := range be {
		le[len(be)-1-i] = b
	}

	matchID := leUint64(le[0:8])
	reservationID := leUint64(le[8:16])
	tvPort := leUint16(le[16:18])

	return DecodedShareCode{MatchID: matchID, ReservationID: reservationID, TVPort: tvPort}, nil
}

func leUint64(b []byte) uint64 {
	var v uint64
	for i := 7; i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}
	return v
}

func leUint16(b []byte) uint16 {
	return uint16(b[0]) | uint16(b[1])<<8
}
