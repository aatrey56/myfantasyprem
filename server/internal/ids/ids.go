// Package ids mints and validates our opaque public ids: a type prefix, an
// underscore and a lowercase ULID, for example lg_01k7m2xq8rfv3c9a4t6wbn5hdy.
//
// A ULID is a 48-bit millisecond timestamp followed by 80 random bits, encoded
// as 26 Crockford base32 characters, so ids sort roughly by creation time.
// Prefixes used by the API contract: usr, lg, tm, mu, plr, fx, txn, drf, pk,
// wv, trd, inv, imp.
package ids

import (
	"crypto/rand"
	"encoding/binary"
	"strings"
	"time"
)

// alphabet is Crockford base32, lowercase: no i, l, o or u.
const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// ulidLen is the length of an encoded ULID.
const ulidLen = 26

// New returns a new id with the given prefix, for example New("lg").
func New(prefix string) string {
	return prefix + "_" + ULID()
}

// ULID returns a new lowercase ULID without a prefix.
func ULID() string {
	return encode(time.Now(), randomBytes())
}

// Valid reports whether s is an id with the given prefix: prefix, underscore
// and exactly 26 characters of the lowercase Crockford alphabet. It matches
// the pattern in api/openapi.yaml.
func Valid(prefix, s string) bool {
	rest, ok := strings.CutPrefix(s, prefix+"_")
	if !ok || len(rest) != ulidLen {
		return false
	}
	for i := 0; i < len(rest); i++ {
		if strings.IndexByte(alphabet, rest[i]) < 0 {
			return false
		}
	}
	return true
}

func randomBytes() [10]byte {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read does not return an error on supported platforms.
		panic("ids: crypto/rand: " + err.Error())
	}
	return b
}

// encode packs 48 timestamp bits and 80 random bits into 128 bits and writes
// them as 26 base32 characters, most significant first. The first character
// therefore carries only 3 bits and is at most '7'.
func encode(t time.Time, r [10]byte) string {
	hi := uint64(t.UnixMilli())<<16 | uint64(binary.BigEndian.Uint16(r[:2]))
	lo := binary.BigEndian.Uint64(r[2:])
	var out [ulidLen]byte
	for i := ulidLen - 1; i >= 0; i-- {
		out[i] = alphabet[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out[:])
}
