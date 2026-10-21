# HGTV

**HGTV 是以 Android TV 為主要目標的短劇應用。** 本專案目前只提供 Android **ARMv7（`armeabi-v7a`）** 建置，不提供 arm64、x86_64、Windows 或 iOS 版本。

## 下載與安裝

前往 [Actions](https://github.com/DickyR15/HGTV/actions) 執行 **Build HGTV Android v7a**，成功後下載工作流程產物 `HGTV-armeabi-v7a.apk`，再安裝到支援 ARMv7 的 Android TV 裝置。

## 功能

- HGTV 品牌圖示與 Android TV 橫幅
- 繁體中文（台灣用語）介面
- 以原專案功能為基礎，保留短劇瀏覽、搜尋、播放及下載功能

## 建置

本機環境需安裝 Flutter 3.47.4、Dart 3.12+、Go 1.24.1+、Python 3.12、JDK 17、Android SDK 與 NDK 28.2.13676358。

```bash
python3 scripts/build_android.py --abi armeabi-v7a
```

## 上游來源

本專案移植自 [Xanerix/guoapp](https://github.com/Xanerix/guoapp)。原作者、第三方依賴與元件的權利及授權仍以各自原始專案為準；散布前請確認相關授權條款。

## 注意事項

- APK 建置成功不代表已在所有電視實機通過播放測試。
- 片源能否播放、畫質與可觀看集數仍受來源網站、網路與授權限制。
