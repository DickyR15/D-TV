package core

import (
	"strings"
	"testing"
)

const escapedBaiduGimgPoster = "https://gimg0.baidu.com/gimg/app=2001&amp;n=0&amp;g=0n&amp;fmt=webp&amp;src=img.baipiaozhe.com/assets/images/07/07ee1e07dfe068858170dfc5ef4df80af77732a31bf6a1567f588624d02562e7.webp"

const decodedBaiduGimgPoster = "https://gimg0.baidu.com/gimg/app=2001&n=0&g=0n&fmt=webp&src=img.baipiaozhe.com/assets/images/07/07ee1e07dfe068858170dfc5ef4df80af77732a31bf6a1567f588624d02562e7.webp"

const unwrappedBaiduPoster = "https://img.baipiaozhe.com/assets/images/07/07ee1e07dfe068858170dfc5ef4df80af77732a31bf6a1567f588624d02562e7.webp"

func TestNativeCoverValueStringsDecodesEscapedBaiduGimgPoster(t *testing.T) {
	values := nativeCoverValueStrings(escapedBaiduGimgPoster)
	if len(values) != 1 {
		t.Fatalf("decoded poster values = %#v, want one value", values)
	}
	if values[0] != decodedBaiduGimgPoster {
		t.Fatalf("decoded poster URL = %q, want %q", values[0], decodedBaiduGimgPoster)
	}
	if got := duoduoBaiduPosterSourceURL(values[0]); got != unwrappedBaiduPoster {
		t.Fatalf("unwrapped Baidu poster = %q, want %q", got, unwrappedBaiduPoster)
	}
}

func TestDuoduoPosterAttemptsIncludeUnwrappedBaiduSource(t *testing.T) {
	attempts := duoduoPosterAttemptsForDrama(
		decodedBaiduGimgPoster,
		nil,
		nativeDrama{ID: "duoduo:157219", Source: sourceDuoduo, SourceID: "157219"},
	)
	for _, attempt := range attempts {
		if attempt.address == unwrappedBaiduPoster {
			return
		}
	}
	var addresses []string
	for _, attempt := range attempts {
		addresses = append(addresses, attempt.address)
	}
	if !strings.Contains(strings.Join(addresses, "\n"), "gimg0.baidu.com") {
		t.Fatalf("unexpected poster attempts: %#v", attempts)
	}
	t.Fatalf("Baidu source URL was not tried; attempts: %#v", attempts)
}
