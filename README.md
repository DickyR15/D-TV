> ⚠️ **免責宣告**：本專案原始碼來自網上大名鼎鼎的**魚佬**（原作者）。我只是把它拿來打包、測試著玩，方便自己用。

# HGTV

Flutter 多端獨立短劇 / 影視應用

Telegram：[交流群組](https://t.me/+c9YKHp7FjSk0MjI1) · [更新頻道](https://t.me/+F9NnQSsMfOU3OTdl)

## 使用

| 功能 | 操作 |
| --- | --- |
| 瀏覽 | 標題欄依次提供排序與篩選、榜單、多選下載和展開搜尋。分類保持獨立一行，內容區左右滑動切類；支援預設、名稱、自然季號、上線日期、熱度、播放量排序及連載狀態篩選。列表下滑接近底部自動載入下一頁，底部「載入更多」為兜底 |
| 搜尋 | 紅果合併官網與名稱索引，輸入停頓 300 毫秒顯示最多 10 條聯想；韓小圈、劇果、野果、鬼片呼叫各自的線上搜尋並按分頁繼續載入。部分失敗保留有效結果；最近 20 次搜尋按使用者儲存 |
| 推薦 | 僅紅果分類欄在「全部」後顯示「推薦」，可切換真人劇、漫劇、AI 劇並分別記住位置 |
| 詳情 | 普通點劇直接進入播放；下載和兜底入口保留詳情頁。詳情頁瀏覽封面、資料、追劇狀態與可展開簡介；底部固定「立即播放 / 繼續播放」和下載入口。選集預設摺疊，展開後每組 50 集，可切換範圍、定位當前或輸入集數跳轉；寬屏與電視採用資料、選集分欄 |
| 追劇與歷史 | 按想看 / 在看 / 已看 / 有更新篩選；紅果已追劇發現新季後進入「有更新」。追劇和歷史均可搜尋。每個使用者獨立儲存，每 5 秒及退出播放時記錄真實進度 |
| 卡片與批次下載 | 卡片「更多」提供收藏、狀態和下載選集；電腦右鍵，電視選單鍵。發現頁長按卡片或點標題欄「多選下載」，一次最多 50 部 |
| VIP | 全站源版瀏覽黃豆或全部站源時顯示 VIP 圖示，僅過濾黃豆；預設隱藏已確認的 VIP。未知、免費、VIP 分別儲存，未知不會覆蓋已知狀態 |
| 手機播放 | 上滑下一集、下滑上一集；長按 350 毫秒臨時 3 倍速。豎屏輕點播放 / 暫停，橫屏輕點控制條，雙擊播放 / 暫停。影片下方為「選集 / 簡介 / 下載」Tab |
| 畫中畫 | Android 手機 / 平板播放器接入畫中畫按鈕；進入前隱藏自繪控制層與彈幕，小窗只保留畫面。Android TV、Windows、iOS 暫未接入 |
| 畫質增強 | 僅 Windows 桌面端顯示並執行增強鏈路（關閉 / 自動 / 省電增強 / 清晰優先）；移動端與電視端隱藏 |
| 彈幕 | 紅果線上播放預設開啟；播放器側邊圓形「彈」字按鈕可關閉、檢視狀態及失敗重試。本地播放不載入彈幕 |
| 下載 | 播放頁「下載」Tab 和詳情頁下載入口均可選擇分集與畫質；支援批次暫停、繼續、重試、刪除及清理任務但保留影片。「更新本劇」補新增或缺失分集 |
| 更多 | 站源管理（全站源版可逐個開關站源）、使用者管理、設定與備份、介面模式、關於 |

| 站源 | 瀏覽與播放 | 搜尋 |
| --- | --- | --- |
| 紅果 | 真人劇、漫劇、AI 劇及分集 | 聯網搜尋與官網搜尋聯想 |
| 韓小圈 | MacCMS 模板：最新韓劇 / 韓國電影 / 韓國綜藝 / 韓國動漫，多線路分集 | 站源線上搜尋 |
| 鬼片 | MacCMS 站點：鬼片 / 電視劇 / 動漫，多線路分集 | RSS 最新條目標題匹配（站點搜尋已停用） |
| 青空 | 番劇、劇場動畫、特攝及分集 | 站源線上搜尋 |
| 黃豆 | 列表、VIP 標記及分集 | 篩選已載入短劇 |
| 劇果 | 熱門、最新、介面分類、詳情及可播放分集；簽名 Cookie 接入線上播放、預載入和下載 | 站源線上搜尋，支援繼續載入 |
| 野果 | 介面實際分類、目錄、詳情及真實分集；按分集重新取流，保留 H.264 / H.265 地址 | 站源線上分頁搜尋 |
| 帝果 | 網頁分類、目錄、詳情及分集；vplayer 簽名解析 | 站源線上分頁搜尋 |
| 黃果影片 / 黃果 AI / 黃果舊版 | 列表、分類、詳情及分集 | 篩選已載入短劇 |

站源可用性、清晰度和區域限制取決於源站及網路；應用不解除源站 VIP 或其他授許可權制。

## 安裝包與平臺狀態

| 平臺 | 包與狀態 |
| --- | --- |
| Android 7.0+ | 三架構（arm64-v8a / armeabi-v7a / x86_64）APK；同一簽名可覆蓋升級，舊證書版本需解除安裝重灌 |
| Windows 10/11 x64 | 完整 ZIP 解壓後執行 `hongguojian.exe` / `zhenguojian.exe`，保留所有 DLL 與 `data`；區域網原生髮現依賴 Windows 10 1903+ |
| iOS 15.1+ | 已加入工程、Go 核心連結、媒體依賴、檔案管理與構建指令碼；iOS 播放頁禁用 media_kit_video 硬體紋理加速以規避 libmpv 渲染崩潰；`CFBundleDisplayName` 使用英文 `HGTV` / `HGTV` 以透過 iloader 等側載工具註冊 App ID 時的名稱校驗，中文名寫入 `zh-Hans.lproj/InfoPlist.strings`（開發快照，未經 Xcode 構建驗證）；待 Xcode 構建與真機驗收，無已簽名 IPA |

`INSTALL_FAILED_NO_MATCHING_ABIS` 表示 APK 與裝置架構不匹配，請更換對應架構安裝包。

### GitHub Actions

工作流只能手動觸發，推送和 PR 不再自動執行檢查。

釋出流程：提升 `pubspec.yaml` 的 `version`，在 `CHANGELOG.md` 寫好同名版本一節（標題如 `## 0.2.72+79`，會寫入 Release 說明），推送到 `main`，再在 Actions 中手動執行 **Build app packages**（只能選 `main`）。工作流先檢查版本：tag `app-v{version}`（`+` 替換為 `-`）或同名 Release 已存在時，直接失敗，不進入構建。然後同時構建三個平臺的兩版（預設與 `--all-sources`）：

| 產物 | 內容 |
| --- | --- |
| `*-android` | 三種架構 APK，必須使用釋出證書籤名並透過指紋校驗 |
| `*-windows` | Windows x64 ZIP |
| `*-ios-unsigned` | arm64 iPhone 未簽名 IPA，下載後自行簽名安裝 |

checks（格式、分析、測試不阻塞）、Android、Windows、iOS 全部成功後才建立 tag 和 Release；任一平臺失敗就不釋出。已有 Release 不會被覆蓋。手動釋出流程為開發快照，尚未完成首次執行驗證。

Android 簽名只使用倉庫 Secrets：`ANDROID_KEYSTORE_BASE64`、`ANDROID_KEYSTORE_PASSWORD`、`ANDROID_KEY_ALIAS`、`ANDROID_KEY_PASSWORD`；同時需要配置倉庫變數 `ANDROID_CERT_SHA256`（證書 SHA-256，冒號和大小寫不限）。缺少任意一項都會發布失敗。CI 透過環境變數把簽名傳給 Gradle；本機構建使用 `android/key.properties`。`key.properties` 和證書檔案不入庫。

## 開發與構建

Flutter `3.47.4`、Dart `3.12+`、Go `1.24.1+`、Python `3.10+`。
Android 需要 JDK 17、SDK 36、NDK `28.2.13676358`；
Windows 需要 Visual Studio C++ 桌面元件及 MinGW-w64 x64；
iOS 需要 macOS、完整 Xcode 和 CocoaPods。

構建指令碼對子程式預設設定 `GOPROXY=https://goproxy.cn,direct`、`GOSUMDB=off`，同名環境變數可覆蓋。

~~~sh
python3 scripts/build_android.py                 # HGTV
python3 scripts/build_android.py --all-sources   # HGTV
python3 scripts/build_android.py --abi arm64-v8a
python scripts/build_windows.py [--all-sources]
~~~

~~~sh
python3 scripts/build_ios.py
python3 scripts/build_ios.py --all-sources
python3 scripts/build_ios.py --core-only [--simulator]
python3 scripts/build_ios.py --export-options /path/to/ExportOptions.plist
~~~

產物在 `dist/android`、`dist/windows`、`dist/ios`，紅果版以 `hongguojian-` 開頭，全站源版以 `zhenguojian-` 開頭。

首次 Android 除錯先編譯對應架構核心，再執行：

~~~sh
python3 scripts/build_native.py --platform android --abi arm64-v8a
flutter pub get --enforce-lockfile
flutter run
~~~

除錯全站源版：先給 `build_native.py` 加 `--all-sources`，再 `flutter run --dart-define=ALL_SOURCES=true`；iOS 對應 `build_ios.py --core-only --all-sources`。指令碼會同步設定 Dart 常量和 Go 編譯引數，應用啟動時校驗二者一致，避免混裝原生庫。

播放器使用 [media_kit](https://github.com/media-kit/media-kit) / libmpv，合併和匯出使用 [FFmpegKit min-gpl](https://github.com/sk3llo/ffmpeg_kit_flutter)（含 GPL 媒體元件）。FFmpegKit 不參與正常播放或下載的轉碼。

### 集中檢查

~~~sh
dart format --output=none --set-exit-if-changed lib test
dart analyze --fatal-infos lib test
flutter test --dart-define=DISABLE_REMOTE_IMAGES=true
flutter test --dart-define=DISABLE_REMOTE_IMAGES=true --dart-define=ALL_SOURCES=true
cd native
go test -race ./...
go test -race -ldflags="-X duanjuapp/native/core.buildAllSources=true" ./...
~~~

## 目錄結構

| 目錄 | 內容 |
| --- | --- |
| `lib` | 頁面、播放器、本地使用者、FFI、下載和媒體處理 |
| `native/core`、`native/bridge` | 獨立站源核心、快取、下載、目錄遷移及 C ABI |
| `android`、`windows`、`ios` | 平臺工程與必要資源 |
| `assets/video_enhancement`、`packages/media_kit_libs_windows_video` | 增強 Shader 與許可、固定 Windows 媒體依賴外掛 |
| `scripts`、`.github/workflows` | 構建與釋出 |
| `test` | 自動化測試 |

## 站源開發約定

站源是 Go 原生 provider（`native/core/provider_*.go`），不是執行期載入的 Python 源。新增站源需接入：`provider_huangguo.go`（常量 / 白名單 / `canonicalProviderSource` / `GetHuangguoChapters`）、`provider_media.go`（baseURL / host 反查 / 播放分派）、`app_categories.go`、`app_cover_metadata.go`、`app_runtime.go`（Config 欄位 + 目錄 / 搜尋 / 詳情分派 + 空頁放行），並在 `lib/models.dart` 登記 `SourceSite`。

注意：Go RE2 正則**不支援 lookahead**。解析多線路播放列表時不能用 `(?=...)` 做分段，否則非捕獲組會消耗下一段開頭；應改用顯式字串截斷（從容器標記之後查詢下一段標記）。


## HGTV 移植說明

此儲存庫基於 [Xanerix/guoapp](https://github.com/Xanerix/guoapp) 移植，保留原始來源與第三方元件授權資訊。應用顯示名稱為 HGTV，介面文字採繁體中文（臺灣用語）。
