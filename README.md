# D TV

**D TV 是 Android TV 專用短劇應用。** 目前只建置 Android ARMv7（`armeabi-v7a`），介面採繁體中文（台灣用語）。

**作者：DickyR15**

## 功能

- 左側快速導覽：新劇、紅果／真人劇／AI 劇／漫劇熱播榜、發現、收藏、播放歷史與設定
- 遙控器搜尋與手機 QR Code 搜尋
- 收藏、播放歷史與繼續觀看
- 不提供影片下載功能

## 取得 APK

至 [GitHub Actions](https://github.com/DickyR15/HGTV/actions) 開啟 **Build D TV Android v7a**。建置成功後，在該次執行的 Artifacts 下載 `D-TV-armeabi-v7a.apk`。

## 上游來源

本專案移植自 [Xanerix/guoapp](https://github.com/Xanerix/guoapp)。原作者與第三方元件的授權仍以各自原始專案為準。

繁體中文（台灣用語）轉換使用 OpenCC 字典，授權與來源見 [OPENCC-NOTICE.md](OPENCC-NOTICE.md)。


## 第五階段：跨平台共用核心

本專案以 `native/core` 作為共用 Go 核心，並由各平台原生播放器與封裝方式承載：

- **Windows x64**：`python scripts/build_windows.py`，輸出免安裝 ZIP。
- **Android Universal**：`python scripts/build_android.py --universal`，輸出同時包含 `arm64-v8a`、`armeabi-v7a`、`x86_64` 的單一 APK。
- **iOS**：在 macOS + Xcode 環境執行 `python scripts/build_ios.py --simulator`；預設為未簽章 IPA，實機安裝需使用 Apple 開發者簽章或自行側載簽署。
- **macOS Universal**：`python scripts/build_macos.py`，首次執行會以 Flutter CLI 產生 macOS Runner 專案，之後建立 arm64 + x86_64 Go 核心與 DMG。

暫時性網路／下載故障可透過 `scripts/retry_command.py --attempts 3 -- <command>` 重試；確定性的 Dart、Go、Kotlin、C/C++ 或連結器錯誤會停止重試，應先修正程式碼再重新建置。跨平台 CI 另有失敗工作重跑機制。


## 片源管理

進階片源與忘記管理員密碼時的本機設定重設方式，請參閱 [片源管理說明](docs/SOURCE-MANAGEMENT.md)。
