package store

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID returns a random UUIDv4 string. crypto/rand failing is unrecoverable
// (the OS entropy source is gone), so it panics rather than returning a
// predictable id.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("store: reading random source: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// qrAlphabet is Crockford base32: no I, L, O, or U, so slugs survive being
// read aloud or retyped from a worn label.
const qrAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewQRSlug returns an opaque local token like "PK-7Q3M-DHKR". It encodes
// nothing: no URL, no personal data, nothing resolvable off-instance. A
// label on a bin in a garage is a public object, and the slug is only ever
// meaningful to the local instance that issued it.
func NewQRSlug() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("store: reading random source: " + err.Error())
	}
	out := make([]byte, 0, 12)
	out = append(out, 'P', 'K', '-')
	for i, v := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, qrAlphabet[int(v)%len(qrAlphabet)])
	}
	return string(out)
}
