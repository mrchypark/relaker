package gateway

import "testing"

func TestSafeErrorHandlesNil(t *testing.T) {
	if got := safeError(nil); got != "" {
		t.Fatalf("safeError(nil) = %q", got)
	}
}
