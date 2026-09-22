package db

import (
	"context"
	"testing"
	"time"
)

func TestTxnCleanupContextIsBounded(t *testing.T) {
	ctx, cancel := txnCleanupContext()
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("transaction cleanup context must have a deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > txnCleanupTimeout {
		t.Fatalf("unexpected cleanup deadline: %s", remaining)
	}
	if ctx.Err() != nil && ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("unexpected cleanup context error: %v", ctx.Err())
	}
}
