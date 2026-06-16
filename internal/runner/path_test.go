package runner

import "testing"

func TestIsParentPathDetectsBothSeparators(t *testing.T) {
	for _, path := range []string{"..", "../escape.sh", `..\escape.sh`} {
		if !isParentPath(path) {
			t.Fatalf("isParentPath(%q) = false", path)
		}
	}
	for _, path := range []string{"scripts/run.sh", "..escape.sh"} {
		if isParentPath(path) {
			t.Fatalf("isParentPath(%q) = true", path)
		}
	}
}
