# D TV 1.0.7 候選版

**狀態：開發中，尚未正式發布**

本文件記錄 1.0.7 候選版第一項工作：修正並補強桌面版線上更新流程。1.0.6 仍是目前正式版本。

## Windows x64

維持免安裝 ZIP 更新方式，不改成安裝程式。更新流程應符合以下順序：

1. 從 GitHub Releases 下載 Windows Portable ZIP；網路逾時、連線中斷及伺服器暫時錯誤最多重試三次。
2. 先寫入暫存檔，檢查檔案大小；GitHub Release 有提供 SHA-256 摘要時，必須比對成功才可進入更新程序。
3. 啟動獨立更新程序，在不關閉 D TV 的情況下先解壓縮、檢查 ZIP 路徑、必要檔案與安裝資料夾寫入權限。
4. 完成準備與驗證後才關閉目前版本；覆蓋前先備份所有將被取代的檔案。
5. 覆蓋後啟動新版並觀察啟動狀態。若更新失敗，停止新版、回復舊檔、移除更新新增檔案，並重新啟動舊版。
6. 更新失敗時留下記錄與備份位置，並開啟該版本 Releases 頁面，方便手動下載及排除問題。

更新記錄位置：`%LOCALAPPDATA%\D TV\updater.log`。

免安裝版應解壓在目前 Windows 使用者可寫入的位置。若資料夾位於需要系統管理員權限的位置，更新程序應在關閉 D TV 之前明確提示，不可直接開始覆蓋。

## Android TV 操作修正

- 密碼欄位按下系統鍵盤「打勾／完成」時，只收起鍵盤並將焦點移到「登入」按鈕。必須再按一次「登入」才會驗證，避免鍵盤的確認鍵被重複當成登入指令。
- 詳情頁「收藏／取消收藏」有獨立焦點；可從底部播放按鈕向左進入，也可從第一欄選集向左移至收藏，並以遙控器確認。

## Windows Google Cast 裝置探索

- Windows 同時使用 nsd 5.0.1 的系統 DNS-SD API 與原本的 dart_cast mDNS 搜尋 _googlecast._tcp，並合併兩路結果、依 IP／連接埠去重。
- DNS-SD 解析接收器名稱、TXT 中的 Cast ID、IPv4 位址與 8009 連接埠；其中一路失敗不會丟棄另一路找到的裝置。
- 自動化測試涵蓋 DNS-SD 服務轉換、缺少 IPv4 時忽略，以及缺少 Cast ID 時的備援識別碼。
- 需用 Windows 實機確認 Edge 可發現的 onn. 4K Pro 也會出現在 D-TV 裝置清單，並實際完成 Google Cast 連線。

## macOS

維持 Universal DMG 流程：App 下載 DMG 並交由系統開啟，使用者將新版 D TV 拖曳至「應用程式」取代舊版。若下載或開啟 DMG 失敗，App 應提供前往 GitHub Releases 版本頁的備援入口。

## iOS

iOS 不嘗試在 App 內安裝 IPA。按下更新後開啟對應版本的 GitHub Releases 頁，讓使用者查看更新說明與 SHA-256 校驗碼，再自行下載未簽章 IPA 並透過側載工具安裝。

## 驗收項目

- Windows：從 1.0.6 更新到候選版、下載中斷後重試、摘要不符、ZIP 缺檔、資料夾無寫入權限、更新準備逾時、新版啟動失敗及舊版回復。
- Windows：確認收藏、觀看紀錄、使用者資料及設定不受程式更新影響。
- macOS：確認 DMG 可開啟、可手動取代舊版，且本機資料保留。
- iOS：確認更新按鈕開啟版本頁，而不是直接下載 IPA。

所有項目完成自動化與實機驗收前，不得將本候選版標示為正式版。


## CI 工具鏈與 Runner 更新

- JavaScript GitHub Actions 升級至 Node.js 24 相容版本：checkout v7.0.1、setup-python v7.1.0、setup-go v7.0.0、setup-java v6.3.0、upload-artifact v7.0.2、download-artifact v8.0.2、setup-android v4.0.4、github-script v9.0.0 及 action-gh-release v3.0.4。
- setup-java 保留 Java problem matcher，Android 建置仍使用 Temurin JDK 17。
- Windows MSYS2 改用 UCRT64 環境及 `mingw-w64-ucrt-x86_64-gcc`；原生核心仍由既有 GCC 呼叫方式編譯。
- Android Gradle Wrapper 改用 Gradle 9.9.1 `bin` 發行套件，並使用固定 SHA-256：`d8a18a7a841dd5df825d35a685fad6f2d7f12ac0c280e57b67c27b4a520ba228`。保留 AGP 9.1.0、Kotlin Plugin 2.4.0 與 JDK 17，不額外變更 Android Plugin 版本。
- Ubuntu jobs 固定使用 `ubuntu-26.04`，避開 `ubuntu-latest` 在 2026 年 10–11 月的 Runner 映像遷移窗口。
- macOS Universal 工作維持 `macos-26`；Runner 排隊警告是 GitHub Hosted Runner 容量因素，不能由專案升級套件解決。
- Android Emulator 安裝／啟動為非阻擋式 best-effort 檢查；APK 編譯、簽章、套件與 ABI 驗證仍不可略過。

