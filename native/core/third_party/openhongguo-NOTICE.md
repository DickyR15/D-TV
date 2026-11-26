# OpenHongguo SDK

D-TV uses the Go module `github.com/beilunyang/openhongguo` at
`v0.4.1` for Redfruit SDK integration.

Upstream: https://github.com/beilunyang/openhongguo
Pinned source commit: `cac5dbc0e1150c0f93e309d8de68bb483f50f499`
License: Apache License 2.0. A copy is included in
`LICENSE.openhongguo.txt`.

D-TV's adapter and mapping code is maintained separately in
`provider_hongguo_sdk.go`. The SDK's protocol code is used as an external Go
module dependency; it is not vendored or modified in this repository.
