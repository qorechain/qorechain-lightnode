package config

import "testing"

func TestValidateOperatorAddress(t *testing.T) {
	good := []string{
		"qor12c8vfzltd6gft3wzx0nr9ekz9yy42waynxhz3f",
		"qor1w8gxlznxqzwykfpfr0l3xjqvm2q2y6y9mysq7h",
	}
	for _, a := range good {
		if err := ValidateOperatorAddress(a); err != nil {
			t.Fatalf("%s rejected: %v", a, err)
		}
	}
	bad := map[string]string{
		"":     "empty",
		"qor1": "no payload",
		"cosmos1w8gxlznxqzwykfpfr0l3xjqvm2q2y6y9mysq7h": "wrong prefix",
		"qor1w8gxlznxqzwykfpfr0l3xjqvm2q2y6y9mysq7g":    "bad checksum",
		"QOR1W8GXLZNXQZWYKFPFR0L3XJQVM2Q2Y6Y9MYSQ7H":    "uppercase",
		"qor1w8gxlznxqzwykfpfr0l3xjqvm2q2y6y9mysq7hb":   "wrong length",
	}
	for a, why := range bad {
		if err := ValidateOperatorAddress(a); err == nil {
			t.Fatalf("%s accepted (%s)", a, why)
		}
	}
}
