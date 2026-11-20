# Windows 獨立更新器

D TV Windows 免安裝版保留完整 Portable ZIP；DTV-Updater.exe 是獨立、以 .NET 10 self-contained single-file 發行的更新程序，並同時附在 Windows ZIP 與 GitHub Release。

## 更新流程

1. D TV 讀取 GitHub 最新正式 Release，確認 Windows ZIP 與 DTV-Updater.exe 都存在。
2. 舊版（尚未內含 updater）會先下載獨立 updater；新版優先使用安裝資料夾內隨包附帶的 updater。下載檔與 Windows ZIP 都會核對 SHA-256。
3. D TV 將 updater 複製到 %LOCALAPPDATA%\D TV\Updater\DTV-Updater.exe，再啟動它並正常結束自己。
4. updater 等待舊版程序結束，檢查 ZIP 雜湊、拒絕路徑穿越、符號連結與重複路徑，解壓並檢查必要執行檔及 DLL。
5. updater 先備份每個將被覆寫的檔案，再安裝完整 Portable ZIP。
6. updater 啟動新版 DTV.exe，透過 Flutter 首幀回報版本及一次性 token；確認版本符合目標版本才視為成功。
7. 成功後刪除下載 ZIP、空的下載資料夾與暫存備份；失敗時停止新版、還原原檔並重新啟動舊版。若還原不完整，保留備份並寫入紀錄。

更新紀錄位於 %LOCALAPPDATA%\D TV\updater.log。已快取的 updater 保留在 %LOCALAPPDATA%\D TV\Updater，供後續更新使用；新版本可安全覆寫安裝資料夾中的舊 updater，因為實際執行的是快取副本。

## 建置與本機驗證

在 Windows 10/11 x64、.NET 10 SDK 環境執行：

    dotnet publish windows_updater/DTVUpdater.csproj -c Release -r win-x64 --self-contained true -p:PublishSingleFile=true -p:IncludeNativeLibrariesForSelfExtract=true -o dist/windows-updater
    dist/windows-updater/DTV-Updater.exe --self-test

GitHub Actions 的 Windows job 會建置 updater、執行 self-test，將它放入 Portable bundle；Release job 再單獨附上同一個 updater，並把其 SHA-256 加入 SHA256SUMS.txt。

## 首次升級相容性

D TV 1.0.7 及更早版本沒有獨立 updater。首次更新仍由當時版本內建的更新流程下載完整 Portable ZIP；新版 ZIP 內已包含 DTV-Updater.exe，因此這次升級完成後就會安裝 updater。後續版本即可使用獨立更新器。若遇到程式版本已具備 updater bootstrap、但安裝資料夾尚未有 updater 的過渡版本，則會從同一個正式 Release 下載並驗證 updater。
