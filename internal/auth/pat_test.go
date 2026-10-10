package auth

import "testing"

func TestPATPrefixAllowsUnderscoresInSecret(t *testing.T) {
	prefix, err := patPrefix("bkk_pat_abc123_secret_with_underscores")
	if err != nil {
		t.Fatal(err)
	}
	if prefix != "abc123" {
		t.Fatalf("prefix=%q", prefix)
	}
}
