package core

import "testing"

func TestSameNativeSystemProxyIncludesAllSettings(t *testing.T) {
	base := nativeSystemProxy{
		HTTP:   "http://proxy.example:8080",
		HTTPS:  "http://proxy.example:8080",
		Bypass: []string{"localhost", "127.*"},
		PAC:    true,
		PACURL: "https://proxy.example/proxy.pac",
	}

	tests := []struct {
		name  string
		value nativeSystemProxy
		want  bool
	}{
		{name: "identical settings", value: base, want: true},
		{
			name: "PAC URL changed",
			value: nativeSystemProxy{
				HTTP: base.HTTP, HTTPS: base.HTTPS, Bypass: base.Bypass,
				PAC: true, PACURL: "https://proxy.example/new-proxy.pac",
			},
			want: false,
		},
		{
			name: "PAC enabled state changed",
			value: nativeSystemProxy{
				HTTP: base.HTTP, HTTPS: base.HTTPS, Bypass: base.Bypass,
				PAC: false, PACURL: base.PACURL,
			},
			want: false,
		},
		{
			name: "HTTP proxy changed",
			value: nativeSystemProxy{
				HTTP: "http://other.example:8080", HTTPS: base.HTTPS,
				Bypass: base.Bypass, PAC: base.PAC, PACURL: base.PACURL,
			},
			want: false,
		},
		{
			name: "HTTPS proxy changed",
			value: nativeSystemProxy{
				HTTP: base.HTTP, HTTPS: "http://other.example:8080",
				Bypass: base.Bypass, PAC: base.PAC, PACURL: base.PACURL,
			},
			want: false,
		},
		{
			name: "bypass list changed",
			value: nativeSystemProxy{
				HTTP: base.HTTP, HTTPS: base.HTTPS,
				Bypass: []string{"localhost"}, PAC: base.PAC, PACURL: base.PACURL,
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sameNativeSystemProxy(base, test.value); got != test.want {
				t.Fatalf("sameNativeSystemProxy() = %v, want %v", got, test.want)
			}
		})
	}
}
