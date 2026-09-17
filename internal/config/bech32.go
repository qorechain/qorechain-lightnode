package config

import (
	"fmt"
	"strings"
)

// A minimal bech32 (BIP-173) verifier, enough to refuse a mistyped operator
// address before it is printed into a chain command. The daemon deliberately
// carries no chain SDK, so this is the only address logic it has: the checksum
// is verified and the payload must be a 20-byte account address.

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

func bech32Polymod(values []byte) uint32 {
	gen := [5]uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := uint32(1)
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (top>>uint(i))&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

func bech32HRPExpand(hrp string) []byte {
	out := make([]byte, 0, 2*len(hrp)+1)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]>>5)
	}
	out = append(out, 0)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]&31)
	}
	return out
}

// ValidateOperatorAddress checks that addr is a well-formed account address
// for this chain: bech32, prefix "qor", valid checksum, 20-byte payload.
func ValidateOperatorAddress(addr string) error {
	if addr != strings.ToLower(addr) {
		return fmt.Errorf("operator address must be lowercase: %q", addr)
	}
	sep := strings.LastIndex(addr, "1")
	if sep < 1 || sep+7 > len(addr) {
		return fmt.Errorf("operator address %q is not bech32", addr)
	}
	hrp, data := addr[:sep], addr[sep+1:]
	if hrp != "qor" {
		return fmt.Errorf("operator address %q does not start with qor1", addr)
	}
	values := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		idx := strings.IndexByte(bech32Charset, data[i])
		if idx < 0 {
			return fmt.Errorf("operator address %q has an invalid character %q", addr, data[i])
		}
		values = append(values, byte(idx))
	}
	if bech32Polymod(append(bech32HRPExpand(hrp), values...)) != 1 {
		return fmt.Errorf("operator address %q has a bad checksum (mistyped?)", addr)
	}
	// 5-bit groups minus the 6 checksum groups, regrouped to bytes.
	payload := values[:len(values)-6]
	bits := len(payload) * 5
	if bits/8 != 20 {
		return fmt.Errorf("operator address %q is not a 20-byte account address", addr)
	}
	return nil
}
