package auth

import (
	"bytes"
	"testing"
)

func TestPurposeBoxSeparatesICloudCredentials(t *testing.T) {
	const key = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	box, err := NewPurposeBox(key, "icloud-calendar")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := NewPurposeBox(key, "other")
	data := []byte("private app password")
	sealed, err := box.Seal(data)
	if err != nil || bytes.Contains(sealed, data) {
		t.Fatal("plaintext not protected")
	}
	plain, err := box.Open(sealed)
	if err != nil || !bytes.Equal(plain, data) {
		t.Fatal("cannot decrypt")
	}
	if _, err := other.Open(sealed); err == nil {
		t.Fatal("ciphertext decrypted with different purpose")
	}
}
