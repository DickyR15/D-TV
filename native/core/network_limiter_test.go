package core

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalogRequestTimeoutIncludesLimiterWait(t *testing.T) {
	limiter := newRequestLimiter(1, 0)
	heldRequest, err := http.NewRequest(http.MethodGet, "https://example.test/held", nil)
	if err != nil {
		t.Fatal(err)
	}
	release, err := limiter.acquire(context.Background(), heldRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	var transportCalls atomic.Int32
	downloader := &Downloader{
		client: &http.Client{
			Transport: sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
				transportCalls.Add(1)
				return sourceFixtureResponse(request, http.StatusOK, "ok"), nil
			}),
		},
		limiter: limiter,
	}

	request, err := http.NewRequest(http.MethodGet, "https://example.test/waiting", nil)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = downloader.doCatalogRequestWithTimeout(request, 40*time.Millisecond)
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected limiter wait to end at request deadline, got %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("request timeout did not bound limiter wait: elapsed=%s", elapsed)
	}
	if transportCalls.Load() != 0 {
		t.Fatalf("network transport started despite limiter deadline: calls=%d", transportCalls.Load())
	}
}
