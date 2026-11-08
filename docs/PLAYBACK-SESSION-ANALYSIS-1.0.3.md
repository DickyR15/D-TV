# D TV 1.0.3 — PlaybackSession 架構評估

## 結論

目前先保留單一前景播放器及既有媒體核心，不在 1.0.3 直接啟用第二個解碼器。現有 PlaybackPreloader 會提早取得下一集播放計畫，並準備播放 URL、manifest 與串流資源；它不是第二個播放器，也沒有提前啟動第二組解碼器。

雙播放器能否降低切集黑屏，取決於目標裝置是否允許同時持有兩個硬體解碼器，以及第二路播放器是否會增加記憶體、CPU/GPU、音訊焦點或音畫同步問題。必須以目標實機測量後再決定。

## 現有播放架構

| 元件 | 現有責任 | 生命週期與資源 |
|---|---|---|
| PlayerScreen | 管理目前集數、播放意圖、播放器狀態、錯誤及畫面控制 | 持有一個前景 Player 與 VideoController；以 generation 區分媒體切換 |
| PlaybackLoader | 解析單集播放計畫、處理逾時及過期請求 | 以 generation 丟棄過期解析結果，並釋放已建立但不再使用的 native session |
| PlaybackPreloader | 預先解析下一集播放計畫 | 約 600 ms 延遲後呼叫 repository.preload；管理預載 session、有效期限、取消及釋放 |
| Native Go stream server | 取得及重寫 HLS manifest、管理串流資源 | 以 session token 管理媒體資源；預載可暖身 manifest／部分串流資源，但不建立第二個解碼器 |
| PlayerInteractions／PlayerControls | 手勢、快捷鍵、控制列、播放與暫停操作 | 目前訂閱同一個前景播放器的事件 |
| Desktop Mini Player | Windows／macOS 獨立小窗播放 | 是獨立視窗與播放器流程，不是隱藏的下一集雙播放器預載 |

## 目前切集流程

1. 收到自動連播或使用者切集要求。
2. _play() 增加 generation，取消舊預載，避免舊請求覆蓋新集數。
3. 透過 _serialize() 排隊執行媒體替換流程。
4. 儲存觀看進度，停止目前播放器，釋放舊 session。
5. 優先接手仍有效的預載計畫；沒有可用預載才重新解析。
6. 將新播放計畫套用到同一個播放器並呼叫 open()。
7. 重新接上播放健康監控、診斷、音訊及區域網播放交接。

切集延遲不只包含 URL 解析，還包含舊 session 停止／釋放、播放器開啟、HLS 首段取得及解碼首畫面等時間。只增加第二個 Player 不一定能解決所有瓶頸。

## 建議的 PlaybackSession 邊界

先建立可測量、可取消、可安全釋放的 session 管理層，再決定是否引入第二個 Player。

狀態流程：

    Idle -> Resolving -> Prepared -> Opening -> Playing
                         |              |         |
                         v              v         v
                      Releasing       Failed   Buffering
                         ^              |         |
                         |              v         v
                         +----------- Recovering <-+
                                      |
                                      v
                                  Resolving

Completed -> Releasing -> Idle
Paused <-> Playing

### Session 物件責任

每個 session 至少需要明確擁有：

- 唯一 session ID、generation、劇目／集數識別及建立時間。
- 播放計畫、憑證到期時間、媒體 URL 與必要 headers。
- 目前擁有資源的 Player／VideoController（如有）。
- 原生 stream session 釋放 callback。
- 播放事件訂閱及取消 token。
- 狀態與錯誤原因；release() 必須具備冪等性，重複釋放不可造成錯誤。
- 資源交接協議：新 session 成功接手前，不可過早釋放仍在播放的舊 session；失敗時必須能回退到現有單播放器流程。

### 必須維持的不變條件

1. 同一個 Player 同一時間只接受一個有效媒體 generation。
2. 過期解析結果不得覆蓋新集數，也不得洩漏 native session。
3. 只有 session owner 可以釋放其 Player、VideoController 及 native session。
4. 取消、切換使用者、離開前景、播放器重建及錯誤恢復都要經過相同的清理路徑。
5. 預載憑證到期時不可直接接手，必須重新解析。
6. 雙播放器試驗不得讓第二個播放器搶走音訊焦點或讓兩路音訊同時輸出。

## 單播放器與雙播放器比較

| 項目 | 單播放器＋現有預載 | 雙播放器預載 |
|---|---|---|
| 下一集 URL／manifest 準備 | 已有 | 已有，可沿用 |
| 下一集解碼器提前啟動 | 否 | 是 |
| 記憶體與 decoder 壓力 | 較低 | 較高，依平台而異 |
| 音訊焦點管理 | 單一 owner | 需要靜音／音軌交接 |
| 切集失敗回復 | 現有 recovery 路徑 | 需要處理兩個 session 的狀態分歧 |
| Android TV 相容性 | 已有 MPV Embed + MediaCodec 基礎 | 必須確認 SoC 同時支援的 decoder 數量與格式 |
| 實作風險 | 較低 | 高，跨 Windows／Android／iOS 的播放器限制不同 |

## 平台風險與測量方式

**Windows（MPV）**
- 確認第二個 MPV 是否建立第二個解碼器及獨立 demuxer cache。
- 測量切集前後 CPU、GPU、RAM、VRAM、開啟時間及首畫面時間。
- 檢查 MPV 的 playing、completed、buffering 事件是否在雙實例間正確隔離。

**Android TV（MPV Embed + MediaCodec）**
- 優先確認裝置 SoC／韌體可同時持有多少個硬體 video decoder，以及目標解析度／編碼格式是否會退回軟體解碼。
- 測量 decoder init failure、RAM、GPU、溫度、首畫面、連續切集及音畫同步。
- 在 Onn. 4K Pro 與其他 Android TV 裝置分開驗證；不能以模擬器結果代替實機。

**iOS**
- 先驗證目前播放路徑與系統提供的解碼資源限制。
- 評估前景／背景生命週期、音訊 session 及裝置旋轉／iPhone Duo 內外螢幕切換期間的資源所有權。

### 建議的實驗矩陣

每種平台／播放器組合都以相同片源、相同解析度及相同網路條件比較：

- 單播放器＋預載（基準組）。
- 第二播放器只建立但不解碼。
- 第二播放器完成 open 並靜音預熱。
- 第二播放器解碼至第一個可呈現影格，但維持靜音。
- 切換時交接音訊及畫面，失敗時立即退回單播放器。

每個組合至少量測：

- 切集到第一個可呈現影格的 P50／P95 時間。
- 切集空白畫面時間、buffering 次數及播放失敗率。
- RAM、CPU、GPU／VRAM、硬體 decoder 使用數量。
- 音訊中斷、重疊、音畫偏移及字幕同步。
- 連續 20 集切換及至少 30 分鐘播放的穩定性。
- 預載失敗、URL 過期、切換帳號、背景／前景切換及強制取消的釋放結果。

### 進入雙播放器實作的門檻

只有在目標裝置的 P95 首畫面延遲有明顯改善，且沒有顯著增加 decoder 失敗、記憶體、音訊重疊、音畫不同步或連續播放錯誤，才將雙播放器列為正式實作。未通過時維持單播放器＋現有預載，並優先改善 session teardown、憑證刷新及串流首段取得。

## 1.0.3 實施範圍

- 本版先完成現有播放器事件時序、generation、session 釋放的回歸測試。
- 保留目前播放器核心，不直接替換 MPV、MPV Embed 或 ExoPlayer。
- 新增可觀察的 session 狀態與效能記錄後，再進行單播放器基準測量。
- 雙播放器只做獨立 POC；除非全部目標平台驗收通過，不合併為正式播放預設。
