package dedupe_test

import (
	"testing"
	"time"

	"github.com/mrchypark/relaker/internal/dedupe"
)

func TestMemoryStoreMarksFirstSeenAndRejectsDuplicate(t *testing.T) {
	store := dedupe.NewMemoryStore()

	dup, key := store.CheckAndMark([]string{"github:delivery-1"})
	if dup {
		t.Fatalf("first CheckAndMark duplicate = true, key = %q", key)
	}

	dup, key = store.CheckAndMark([]string{"github:delivery-1"})
	if !dup {
		t.Fatal("second CheckAndMark duplicate = false")
	}
	if key != "github:delivery-1" {
		t.Fatalf("duplicate key = %q", key)
	}
}

func TestMemoryStoreChecksAllKeysBeforeMarking(t *testing.T) {
	store := dedupe.NewMemoryStore()
	if dup, _ := store.CheckAndMark([]string{"slack:event-1"}); dup {
		t.Fatal("unexpected duplicate")
	}

	dup, key := store.CheckAndMark([]string{"slack:event-2", "slack:event-1"})
	if !dup {
		t.Fatal("expected duplicate when any key was already seen")
	}
	if key != "slack:event-1" {
		t.Fatalf("duplicate key = %q", key)
	}

	dup, _ = store.CheckAndMark([]string{"slack:event-2"})
	if dup {
		t.Fatal("new key was marked even though previous multi-key call was duplicate")
	}
}

func TestMemoryStoreExpiresOldKeys(t *testing.T) {
	store := dedupe.NewMemoryStoreWithTTL(10 * time.Millisecond)

	if dup, key := store.CheckAndMark([]string{"github:delivery-1"}); dup {
		t.Fatalf("first CheckAndMark duplicate = true, key = %q", key)
	}
	time.Sleep(25 * time.Millisecond)

	if dup, key := store.CheckAndMark([]string{"github:delivery-1"}); dup {
		t.Fatalf("CheckAndMark after TTL duplicate = true, key = %q", key)
	}
}
