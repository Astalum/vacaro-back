package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

func NewPurposeBox(encodedKey, purpose string) (*Box, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 || purpose == "" {
		return nil, errors.New("invalid purpose encryption key")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("vacaro:" + purpose + ":v1"))
	return NewBox(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}
