# OpenHongguo SDK

D-TV uses the Go module `github.com/beilunyang/openhongguo` at
`v0.0.0-20261118120351-33c888d3a8a2` for anonymous Redfruit catalogue-season
lookup.

Upstream: https://github.com/beilunyang/openhongguo
Pinned source commit: `33c888d3a8a2db3ea1ff2f1dce5c479c15bf7626`
License: Apache License 2.0. A copy is included in
`LICENSE.openhongguo.txt`.

D-TV's adapter and mapping code is maintained separately in
`provider_hongguo_sdk.go`. The SDK's protocol code is used as an external Go
module dependency; it is not vendored or modified in this repository.
