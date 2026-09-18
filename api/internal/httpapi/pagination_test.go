package httpapi

import "testing"

func TestPaginationCursorsAreOpaqueAndRoundTrip(t *testing.T) {
	encoded := encodeCursor(25)
	if encoded == "25" {
		t.Fatal("cursor must not expose a numeric offset")
	}
	decoded, err := cursorOffset(encoded)
	if err != nil || decoded != 25 {
		t.Fatalf("cursor round trip = %d, %v", decoded, err)
	}
	if _, err := cursorOffset("25"); err == nil {
		t.Fatal("numeric cursor should be rejected")
	}
}
