package bridge

import "testing"

func TestMaxWebSocketMessageSize(t *testing.T) {
	if maxWebSocketMessageSize != 5<<20 {
		t.Fatalf("expected 5 MiB WebSocket limit, got %d", maxWebSocketMessageSize)
	}
}
