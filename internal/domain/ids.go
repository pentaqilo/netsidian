package domain

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

// Sortable prefixed identifiers (ULID-like): 48-bit big-endian millisecond
// timestamp + 80 random bits, base32 (Crockford-style lowercase alphabet).
// IDs sort lexicographically by creation time, giving deterministic
// pagination order. Encoded length is 26 characters (48+80 bits = 26x5).
const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"

var (
	idMu       sync.Mutex
	lastMillis int64
	lastRnd    [10]byte
)

func NewID(prefix string) string {
	idMu.Lock()
	defer idMu.Unlock()
	ms := time.Now().UnixMilli()
	var rnd [10]byte
	if ms <= lastMillis {
		// stay monotonic within the same millisecond
		ms = lastMillis
		rnd = lastRnd
		incrnd(&rnd)
	} else {
		if _, err := rand.Read(rnd[:]); err != nil {
			panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
		}
	}
	if bytesZero(rnd[:]) { // wrapped: advance the millisecond
		ms++
		if _, err := rand.Read(rnd[:]); err != nil {
			panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
		}
	}
	lastMillis = ms
	lastRnd = rnd

	out := make([]byte, 0, 26)
	out = append(out, encode5([]byte{
		byte(ms >> 40), byte(ms >> 32), byte(ms >> 24),
		byte(ms >> 16), byte(ms >> 8), byte(ms),
	})...)
	out = append(out, encode5(rnd[:])...)
	return prefix + "_" + string(out)
}

func incrnd(r *[10]byte) {
	for i := len(r) - 1; i >= 0; i-- {
		r[i]++
		if r[i] != 0 {
			return
		}
	}
}

func bytesZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// encode5 converts bytes into base32 chars, 5 bits per char.
func encode5(b []byte) []byte {
	out := make([]byte, 0, (len(b)*8+4)/5)
	bits, acc := 0, 0
	for _, v := range b {
		acc = acc<<8 | int(v)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, alphabet[(acc>>bits)&0x1f])
		}
	}
	if bits > 0 {
		out = append(out, alphabet[(acc<<(5-bits))&0x1f])
	}
	return out
}
