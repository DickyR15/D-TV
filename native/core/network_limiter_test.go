package core

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func requestLimiterTestRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "https://media.example.test/video", nil)
}

func requestLimiterHasBackoff(limiter *requestLimiter, host string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	_, exists := limiter.backoffs[host]
	return exists
}

func TestRequestLimiterWaitsForThreeConsecutiveForbiddenResponses(t *testing.T) {
	limiter := newRequestLimiter(2, 0)
	request := requestLimiterTestRequest()
	response := &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header)}

	for attempt := 1; attempt < forbiddenStreakLimit; attempt++ {
		limiter.observe(request, response)
		if requestLimiterHasBackoff(limiter, request.URL.Hostname()) {
			t.Fatalf("attempt %d unexpectedly activated host cooldown", attempt)
		}
	}

	limiter.observe(request, response)
	if !requestLimiterHasBackoff(limiter, request.URL.Hostname()) {
		t.Fatal("third consecutive 403 should activate host cooldown")
	}
}

func TestRequestLimiterResetsForbiddenStreakAfterNonForbiddenResponse(t *testing.T) {
	limiter := newRequestLimiter(2, 0)
	request := requestLimiterTestRequest()
	forbidden := &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header)}
	success := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}

	limiter.observe(request, forbidden)
	limiter.observe(request, forbidden)
	limiter.observe(request, success)
	limiter.observe(request, forbidden)
	limiter.observe(request, forbidden)

	if requestLimiterHasBackoff(limiter, request.URL.Hostname()) {
		t.Fatal("non-403 response should reset the consecutive 403 streak")
	}
	limiter.mu.Lock()
	streak := limiter.forbiddenStreaks[request.URL.Hostname()]
	limiter.mu.Unlock()
	if streak != 2 {
		t.Fatalf("expected reset streak to be 2, got %d", streak)
	}
}

func TestRequestLimiterKeepsImmediateRateLimitAndChallengeProtection(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header string
	}{
		{name: "rate limit", status: http.StatusTooManyRequests},
		{name: "browser challenge", status: http.StatusForbidden, header: "challenge"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limiter := newRequestLimiter(2, 0)
			request := requestLimiterTestRequest()
			response := &http.Response{StatusCode: test.status, Header: make(http.Header)}
			if test.header != "" {
				response.Header.Set("Cf-Mitigated", test.header)
			}

			limiter.observe(request, response)
			if !requestLimiterHasBackoff(limiter, request.URL.Hostname()) {
				t.Fatal("explicit rate limit and browser challenge should activate immediate protection")
			}
		})
	}
}
