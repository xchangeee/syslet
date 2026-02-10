package util

import "testing"

func TestSha256hex_ReturnsStringInHexFormat(t *testing.T) {
	result := Sha256hex([]byte("hello"))
	expected := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if result != expected {
		t.Errorf("Sha256hex(%q) = %q, want %q", "hello", result, expected)
	}
}
