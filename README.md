# D TV

**D TV 是跨平台短劇播放應用**，採用繁體中文（台灣用語），並針對 Android TV 遙控器操作進行最佳化。

**作者：DickyR15**

## 支援平台

| 平台 | 發布格式 | 說明 |
|---|---|---|
| Android／Android TV | Universal APK | 單一 APK，包含 ARM64、ARMv7 與 x86_64 |
| Windows x64 | Portable ZIP | 解壓縮後即可執行，無須安裝 |
| macOS | Universal DMG | 支援 Apple Silicon 與 Intel |
| iOS | IPA | 未簽章 IPA，需自行簽署後側載安裝 |

## 功能

- 紅果熱播榜、真人劇熱播榜、AI 劇熱播榜及漫劇熱播榜
- 分類瀏覽、繁體中文關鍵字搜尋與手機 QR Code 搜尋
- 收藏、觀看紀錄及繼續觀看
- Android TV 遙控器焦點導覽；海報短按播放、長按立即開啟操作選單
- 線上串流播放與播放來源管理
- 介面配色、播放設定、畫面調整及跨平台操作介面
- 使用者管理、設定備份與還原
- 不提供影片下載功能

## 下載正式版

前往 [D TV 1.0.1 正式版 Releases](https://github.com/DickyR15/D-TV/releases/tag/v1.0.1)，依照裝置選擇對應檔案：

- **Android／Android TV**：`DTV-1.0.1-Android.apk`
- **Windows x64**：`DTV-1.0.1-Windows-Portable.zip`
- **macOS**：`DTV-1.0.1-macOS.dmg`
- **iOS**：`DTV-1.0.1-iOS.ipa`（未簽章，需使用 AltStore、Sideloadly 等方式自行簽署後安裝）

Android Universal APK 包含 `arm64-v8a`、`armeabi-v7a` 與 `x86_64` 三種 ABI。Android 更新時使用相同正式簽署金鑰；若安裝時出現簽章不相容，請勿先移除舊版，以免遺失本機資料，應先確認安裝檔來源及簽署憑證。

## 開發與建置

本專案使用 Flutter 作為跨平台介面，並以 `native/core` 作為共用 Go 核心。

- Windows x64：`python scripts/build_windows.py`
- Android Universal：`python scripts/build_android.py --universal`
- iOS：`python scripts/build_ios.py --simulator`（需 macOS、Xcode 與 CocoaPods；預設輸出未簽章 IPA）
- macOS Universal：`python scripts/build_macos.py`

跨平台 CI 會執行 Flutter／Go 測試、各平台建置及必要的封裝檢查。Android 建置另驗證 APK 簽章、套件識別與三種 ABI，並嘗試在 Android 模擬器安裝啟動。

## 上游來源與授權

本專案參考並移植自 [Xanerix/guoapp](https://github.com/Xanerix/guoapp)。原作者與第三方元件的授權仍以各自原始專案為準。

繁體中文（台灣用語）轉換使用 OpenCC 字典，授權與來源見 [OPENCC-NOTICE.md](OPENCC-NOTICE.md)。其他第三方元件與媒體解碼元件請依各自授權條款使用。

## 文件

進階片源管理與忘記管理員密碼時的本機設定重設方式，請參閱 [片源管理說明](docs/SOURCE-MANAGEMENT.md)。
