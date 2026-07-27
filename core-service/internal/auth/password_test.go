package auth

import "testing"

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("a secure password")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "a secure password") {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(hash, "a different password") {
		t.Fatal("invalid password accepted")
	}
}

func TestPasswordHashRejectsMalformedValues(t *testing.T) {
	for _, hash := range []string{
		"",
		"$argon2id$v=19$m=1,t=1,p=1$bad$bad",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5",
	} {
		if VerifyPassword(hash, "password") {
			t.Fatalf("malformed hash accepted: %q", hash)
		}
	}
}
