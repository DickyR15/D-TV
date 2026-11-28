module duanjuapp/native

go 1.27.4

require (
	github.com/beilunyang/openhongguo v0.4.1
	github.com/dop251/goja v0.0.0-20250630131328-58d95d85e994
	github.com/bogdanfinn/fhttp v0.6.9
	github.com/bogdanfinn/tls-client v1.16.0
	golang.org/x/net v0.48.0
	golang.org/x/text v0.32.0
)

require (
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/bdandy/go-errors v1.2.2 // indirect
	github.com/bdandy/go-socks4 v1.2.3 // indirect
	github.com/bogdanfinn/quic-go-utls v1.0.10-utls // indirect
	github.com/bogdanfinn/utls v1.7.8-barnius // indirect
	github.com/bogdanfinn/websocket v1.5.6-barnius // indirect
	github.com/cloudflare/circl v1.6.2 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/google/pprof v0.0.0-20250630185457-6e76a2b096b5 // indirect
	github.com/klauspost/compress v1.18.2 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/tam7t/hpkp v0.0.0-20160821193359-2b70b4024ed5 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
)

// Temporary candidate-only SDK compatibility fix: upstream PR #25.
// Exact fork commit, based on v0.4.1; remove when the fix is released upstream.
replace github.com/beilunyang/openhongguo => github.com/J3n5en/openhongguo v0.0.0-20261124180309-09a0160f6f72
