package auth

import "encoding/base64"

// b64Decode is a small helper to keep auth.go's top imports tidy.
func b64Decode(s string) (string, error) {
	dec, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(dec), nil
}
