package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNativeTestResourceConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	result, err := nativeTestResourceConnection(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if result["statusCode"] != http.StatusNoContent {
		t.Fatalf("statusCode = %v, want 204", result["statusCode"])
	}
	if _, ok := result["elapsedMs"].(int64); !ok {
		t.Fatalf("elapsedMs type = %T, want int64", result["elapsedMs"])
	}
}

func TestNativeTestResourceConnectionRejectsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	if _, err := nativeTestResourceConnection(context.Background(), server.Client(), server.URL); err == nil {
		t.Fatal("expected non-2xx response to fail")
	}
}

func TestNativeTestResourceConnectionRequiresClient(t *testing.T) {
	if _, err := nativeTestResourceConnection(context.Background(), nil, nativeResourceConnectionTestURL); err == nil {
		t.Fatal("expected nil client to fail")
	}
}
