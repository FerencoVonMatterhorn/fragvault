package matches

import (
	"math/big"
	"testing"
)

// These cover format validation and a structural round-trip, which we can
// check with confidence. Correctness against Valve's *real* sharecodes still
// needs to be validated against a live account (see /docs/architecture.md) —
// a round-trip through our own encode logic only proves decode is the
// correct inverse of encode as we understand the format, not that our
// understanding of the format matches Valve's.

func TestDecodeShareCode_RejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"not-a-sharecode",
		"CSGO-short",
		"CSGO-AbC12-dEf34-Gh567-iJk89", // only 4 groups
		"CSGO-AbC12-dEf34-Gh567-iJk89-LmN0!",
	}
	for _, c := range cases {
		if _, err := DecodeShareCode(c); err == nil {
			t.Errorf("DecodeShareCode(%q): expected error, got none", c)
		}
	}
}

// encodeShareCodeForTest is the inverse of DecodeShareCode's base57 step,
// used only to build a fixture for the round-trip test below.
func encodeShareCodeForTest(matchID, reservationID uint64, tvPort uint16) string {
	le := make([]byte, 18)
	putLE64(le[0:8], matchID)
	putLE64(le[8:16], reservationID)
	putLE16(le[16:18], tvPort)

	be := make([]byte, 18)
	for i, b := range le {
		be[len(le)-1-i] = b
	}

	// Decode treats stripped[0] as the least-significant base57 digit (see
	// DecodeShareCode), so the first remainder we peel off goes at index 0.
	acc := new(big.Int).SetBytes(be)
	digits := make([]byte, 25)
	for i := 0; i <= 24; i++ {
		var rem big.Int
		acc.DivMod(acc, dictLen, &rem)
		digits[i] = dictionary[rem.Int64()]
	}
	body := string(digits)
	return "CSGO-" + body[0:5] + "-" + body[5:10] + "-" + body[10:15] + "-" + body[15:20] + "-" + body[20:25]
}

func putLE64(b []byte, v uint64) {
	for i := 0; i < 8; i++ {
		b[i] = byte(v)
		v >>= 8
	}
}

func putLE16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func TestDecodeShareCode_RoundTrip(t *testing.T) {
	cases := []DecodedShareCode{
		{MatchID: 1, ReservationID: 1, TVPort: 1},
		{MatchID: 0, ReservationID: 0, TVPort: 0},
		{MatchID: 3186428574821451776, ReservationID: 8912345678901234, TVPort: 55563},
		{MatchID: 0xFFFFFFFFFFFFFFFF, ReservationID: 0xFFFFFFFFFFFFFFFF, TVPort: 0xFFFF},
	}
	for _, want := range cases {
		code := encodeShareCodeForTest(want.MatchID, want.ReservationID, want.TVPort)
		got, err := DecodeShareCode(code)
		if err != nil {
			t.Fatalf("DecodeShareCode(%q): unexpected error: %v", code, err)
		}
		if got != want {
			t.Errorf("DecodeShareCode(%q) = %+v, want %+v", code, got, want)
		}
	}
}
