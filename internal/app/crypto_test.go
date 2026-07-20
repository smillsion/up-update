package app

import "testing"

func TestVaultRoundTrip(t *testing.T) {
	vault, err := NewVault([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := vault.Encrypt("SESSDATA=secret")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "SESSDATA=secret" || encrypted == "" {
		t.Fatal("secret was not encoded")
	}
	plain, err := vault.Decrypt(encrypted)
	if err != nil || plain != "SESSDATA=secret" {
		t.Fatalf("round trip failed: %q, %v", plain, err)
	}
}

func TestPasswordHash(t *testing.T) {
	hash, err := hashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(hash, "a-long-test-password") {
		t.Fatal("valid password rejected")
	}
	if verifyPassword(hash, "wrong-password") {
		t.Fatal("invalid password accepted")
	}
}
