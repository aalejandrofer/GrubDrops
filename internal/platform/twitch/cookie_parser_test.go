package twitch

import (
	"testing"
)

func TestParsePickleCookies(t *testing.T) {
	// Test data: a simplified pickle SimpleCookie structure
	// This is a minimal example - real data would be more complex
	testData := []byte{
		0x80, 0x04, 0x95, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // PROTO 4 + FRAME
	}

	// For now, just test that the function doesn't panic
	// A real test would need actual pickle data
	_, err := ParsePickleCookies(testData)
	if err != nil {
		// Expected - the test data is not valid pickle
		t.Logf("Expected error: %v", err)
	}
}

func TestParsePickleCookiesFromBase64(t *testing.T) {
	// Test with empty base64
	_, err := ParsePickleCookiesFromBase64("")
	if err == nil {
		t.Error("Expected error for empty base64")
	}

	// Test with invalid base64
	_, err = ParsePickleCookiesFromBase64("not-valid-base64!!!")
	if err == nil {
		t.Error("Expected error for invalid base64")
	}
}
