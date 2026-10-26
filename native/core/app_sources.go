package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type nativeSourceTask struct {
	cancel context.CancelFunc
}

// hongguoSourceUpdateKey identifies the explicit Red Fruit update task. It lets
// catalog loading retain the pre-website-history update boundary without
// disabling website history for normal catalog browsing.
type hongguoSourceUpdateKey struct{}

type nativeSourceRecord struct {
	Operation       string               `json:"operation"`
	Running         bool                 `json:"running"`
	Stage           string               `json:"stage"`
	Completed       int                  `json:"completed"`
	Total           int                  `json:"total"`
	Added           int                  `json:"added"`
	Error           string               `json:"error,omitempty"`
	NeedsSave       bool                 `json:"needsSave,omitempty"`
	StartedAt       time.Time            `json:"startedAt"`
	FinishedAt      time.Time            `json:"finishedAt"`
	RetryAt         time.Time            `json:"retryAt"`
	Health          *nativeSourceHealth  `json:"health,omitempty"`
	MetadataChecked map[string]time.Time `json:"metadataChecked,omitempty"`
	MetadataRetryAt map[string]time.Time `json:"metadataRetryAt,omitempty"`
	MetadataCursor  string               `json:"metadataCursor,omitempty"`
}

type nativeSourceStatus struct {
	UnknownVIP   int                 `json:"unknownVip"`
	Source       string              `json:"source"`
	Count        int                 `json:"count"`
	Page         int                 `json:"page"`
	HasMore      bool                `json:"hasMore"`
	UpdatedAt    time.Time           `json:"updatedAt"`
	Operation    string              `json:"operation"`
	Running      bool                `json:"running"`
	Stage        string              `json:"stage"`
	Completed    int                 `json:"completed"`
	Total        int                 `json:"total"`
	Added        int                 `json:"added"`
	Error        string              `json:"error,omitempty"`
	StorageError string              `json:"storageError,omitempty"`
	StartedAt    time.Time           `json:"startedAt"`
	FinishedAt   time.Time           `json:"finishedAt"`
	RetryAt      time.Time           `json:"retryAt"`
	Health       *nativeSourceHealth `json:"health,omitempty"`
}

func (engine *nativeEngine) lockSourceCatalog(ctx context.Context, source string) (func(), error) {
	engine.sourceCatalogMu.Lock()
	if engine.sourceCatalogs == nil {
		engine.sourceCatalogs = map[string]chan struct{}{}
	}
	gate := engine.sourceCatalogs[source]
	if gate == nil {
		gate = make(chan struct{}, 1)
		engine.sourceCatalogs[source] = gate
	}
	engine.sourceCatalogMu.Unlock()
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (engine *nativeEngine) loadSourceRecords() {
	engine.sourceRecords = map[string]nativeSourceRecord{}
	engine.sourceTasks = map[string]*nativeSourceTask{}
	path := filepath.Join(engine.directory, "sources.json")
	info, err := os.Stat(path)
	if err != nil || info.Size() > nativeSourceMaxBytes {
		return
	}
	body, err := os.ReadFile(path)
	var records map[string]nativeSourceRecord
	if err != nil || json.Unmarshal(body, &records) != nil {
		return
	}
	for source, record := range records {
		if !isHuangguoProviderSource(source) {
			continue
		}
		if record.Running {
			record.Running = false
			record.Stage = "任务已中断"
			record.Error = "上次任务未完成，已保留已更新内容，可重新开始"
		}
		if record.NeedsSave {
			record.NeedsSave = false
			record.Stage = "上次保存未完成"
			record.Error = "已恢复可读取的缓存，请重新更新"
			for key, state := range engine.catalogStates {
				if key == source || strings.HasPrefix(key, source+"|") {
					state.UpdatedAt = time.Time{}
					engine.catalogStates[key] = state
				}
			}
		}
		engine.sourceRecords[source] = record
	}
}

func (engine *nativeEngine) saveSourceRecordsLocked() error {
	body, err := json.Marshal(engine.sourceRecords)
	if err == nil && len(body) > nativeSourceMaxBytes {
		err = errNativeSourceLimit
	}
	if err == nil {
		err = writeNativeCacheFile(filepath.Join(engine.directory, "sources.json"), body)
	}
	engine.sourceSaveError = nativeSaveError("站源任务记录", err)
	return engine.sourceSaveError
}

func (engine *nativeEngine) sourceStatus(source string) nativeSourceStatus {
	source = canonicalProviderSource(source)
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.sourceStatusLocked(source)
}

func (engine *nativeEngine) sourceStatusLocked(source string) nativeSourceStatus {
	record := engine.sourceRecords[source]
	state, found := engine.catalogStates[source]
	warning := engine.storageWarningLocked()
	stage := record.Stage
	unknownVIP := 0
	if source == sourceHuangdou {
		for _, drama := range engine.catalogs[source] {
			if drama.VIP == nil {
				unknownVIP++
			}
		}
	}
	if warning != "" && !record.Running && stage == "已完成" {
		stage = "等待保存"
	}
	return nativeSourceStatus{Source: source, Count: len(engine.catalogs[source]), UnknownVIP: unknownVIP, Page: max(1, state.Page), HasMore: !found || state.HasMore,
		UpdatedAt: state.UpdatedAt, Operation: record.Operation, Running: record.Running, Stage: stage,
		Completed: record.Completed, Total: record.Total, Added: record.Added, Error: record.Error,
		StorageError: warning,
		StartedAt:    record.StartedAt, FinishedAt: record.FinishedAt, RetryAt: record.RetryAt, Health: record.Health}
}

func (engine *nativeEngine) changeSourceRecord(source string, change func(*nativeSourceRecord)) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.sourceRecords == nil {
		engine.sourceRecords = map[string]nativeSourceRecord{}
	}
	record := engine.sourceRecords[source]
	change(&record)
	engine.sourceRecords[source] = record
	engine.saveSourceRecordsLocked()
}

func (engine *nativeEngine) startSourceTask(source, operation string, drama nativeDrama) (nativeSourceStatus, error) {
	source = canonicalProviderSource(source)
	if !nativeSourceAvailable(source) {
		return nativeSourceStatus{}, errNativeBuildSource
	}
	switch operation {
	case "update", "more", "metadata", "vipMetadata", "check", "checkCatalog", "retrySave":
	default:
		return nativeSourceStatus{}, errors.New("无效的站源操作")
	}
	if operation == "vipMetadata" && source != sourceHuangdou {
		return nativeSourceStatus{}, errors.New("当前站源不需要补齐 VIP 资料")
	}
	if drama.ID != "" && (!nativeDramaAvailable(drama) || sourceFromDramaID(drama.ID) != source) {
		return nativeSourceStatus{}, errNativeBuildSource
	}
	if operation == "retrySave" {
		return engine.retrySourceSave(source), nil
	}
	engine.mu.Lock()
	if engine.sourceTasks == nil {
		engine.sourceTasks = map[string]*nativeSourceTask{}
	}
	if task := engine.sourceTasks[source]; task != nil {
		status := engine.sourceStatusLocked(source)
		engine.mu.Unlock()
		return status, nil
	}
	if engine.sourceRecords == nil {
		engine.sourceRecords = map[string]nativeSourceRecord{}
	}
	previous := engine.sourceRecords[source]
	if time.Now().Before(previous.RetryAt) {
		engine.mu.Unlock()
		return nativeSourceStatus{}, fmt.Errorf("站源暂时暂停请求，请在 %d 秒后重试", max(1, int(time.Until(previous.RetryAt).Seconds()+.999)))
	}
	timeout := 3 * time.Minute
	if canonicalProviderSource(source) == sourceHongguo && (operation == "update" || operation == "metadata") {
		// Red Fruit updates may walk thousands of entries and pause during playback.
		// Per-request deadlines and persisted cursors bound individual work; this
		// outer deadline avoids forcing a restart after each short viewing session.
		timeout = 12 * time.Hour
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	task := &nativeSourceTask{cancel: cancel}
	engine.sourceTasks[source] = task
	previous.Operation, previous.Running, previous.Stage = operation, true, "准备中"
	previous.Completed, previous.Total, previous.Added = 0, 0, 0
	previous.NeedsSave = false
	previous.Error, previous.StartedAt, previous.FinishedAt, previous.RetryAt = "", time.Now(), time.Time{}, time.Time{}
	engine.sourceRecords[source] = previous
	engine.saveSourceRecordsLocked()
	status := engine.sourceStatusLocked(source)
	engine.mu.Unlock()
	go engine.runSourceTask(ctx, source, operation, drama, task)
	return status, nil
}

func (engine *nativeEngine) cancelSourceTask(source string) nativeSourceStatus {
	source = canonicalProviderSource(source)
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if task := engine.sourceTasks[source]; task != nil {
		task.cancel()
		record := engine.sourceRecords[source]
		record.Stage = "正在停止"
		engine.sourceRecords[source] = record
	}
	return engine.sourceStatusLocked(source)
}

func sourceTaskErrorMessage(taskContextErr, err error) string {
	if errors.Is(err, context.Canceled) {
		return "已保留更新內容，可稍後繼續"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		if errors.Is(taskContextErr, context.DeadlineExceeded) {
			return "本次片源任務超時，已保留更新內容，可繼續更新"
		}
		return "單一步驟連線逾時，已保留已完成內容；可重新檢測或稍後重試"
	}
	return publicError(err).Error()
}

func (engine *nativeEngine) runSourceTask(ctx context.Context, source, operation string, drama nativeDrama, task *nativeSourceTask) {
	var err error
	defer func() {
		if recover() != nil {
			err = errors.New("站源任务处理失败，已保留缓存")
		}
		taskContextErr := ctx.Err()
		task.cancel()
		engine.mu.Lock()
		defer engine.mu.Unlock()
		delete(engine.sourceTasks, source)
		record := engine.sourceRecords[source]
		record.Running, record.FinishedAt = false, time.Now()
		record.Stage = "已完成"
		if err != nil {
			record.Stage, record.Error = "未完成", sourceTaskErrorMessage(taskContextErr, err)
			var persistence *nativePersistenceError
			if errors.As(err, &persistence) {
				record.NeedsSave, record.Error = engine.catalogSaveError != nil, ""
				record.Stage = "已保存，可继续更新"
				if record.NeedsSave {
					record.Stage = "等待保存"
				}
			}
			if errors.Is(err, context.Canceled) {
				record.Stage, record.Error = "已停止", "已保留更新内容，可稍后继续"
			}
			if errors.Is(err, context.DeadlineExceeded) &&
				errors.Is(taskContextErr, context.DeadlineExceeded) {
				record.Error = "本次片源任務超時，已保留更新內容，可繼續更新"
			}
			var backoff *requestBackoff
			if errors.As(err, &backoff) {
				record.RetryAt = backoff.until
			}
		}
		engine.sourceRecords[source] = record
		engine.saveSourceRecordsLocked()
	}()
	if operation == "check" || operation == "checkCatalog" {
		err = engine.checkSource(ctx, source, drama, operation == "check")
	} else {
		ctx = context.WithValue(ctx, backgroundCatalogKey{}, true)
		err = engine.updateSource(ctx, source, operation)
	}
}

// sourceUpdateHistoryPageLimit bounds historical paging for each source.
 // Red Fruit's catalog request already scans several feed pages internally, so
 // treating its outer page as if it were one remote page multiplies the work.
 // Keep other providers' configured batch-update behavior unchanged.
func sourceUpdateHistoryPageLimit(source, operation string, maxPages int) int {
	source = canonicalProviderSource(source)
	switch operation {
	case "more":
		return 1
	case "update":
		if source == sourceHongguo {
			return 1
		}
		if maxPages <= 0 {
			maxPages = defaultConfig().MaxPagesPerSort
		}
		return max(0, maxPages-1)
	default:
		return 0
	}
}

func sourceHistoryPageContext(ctx context.Context, source string) context.Context {
	if canonicalProviderSource(source) == sourceHongguo {
		// A historical catalog request for Red Fruit must only fetch one remote
		// page per feed. The background mode intentionally scans up to 50.
		return context.WithValue(ctx, backgroundCatalogKey{}, false)
	}
	return ctx
}

func (engine *nativeEngine) waitForPlaybackIdle(ctx context.Context, source, resumeStage string) error {
	paused := false
	for {
		engine.mu.Lock()
		active := len(engine.playbacks) > 0
		engine.mu.Unlock()
		if !active {
			if paused {
				engine.changeSourceRecord(source, func(record *nativeSourceRecord) {
					record.Stage = resumeStage
				})
			}
			return nil
		}
		if !paused {
			engine.changeSourceRecord(source, func(record *nativeSourceRecord) {
				record.Stage = "播放中，暫停目錄更新"
			})
			paused = true
		}
		timer := time.NewTimer(400 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func waitForSourceCatalogInterval(ctx context.Context) error {
	timer := time.NewTimer(300 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (engine *nativeEngine) updateSource(ctx context.Context, source, operation string) error {
	if canonicalProviderSource(source) == sourceHongguo && operation == "update" {
		ctx = context.WithValue(ctx, hongguoSourceUpdateKey{}, true)
	}
	before := engine.nativeCached(source)
	count := len(before.Items)
	var knownBefore map[string]struct{}
	if source == sourceHongguo && operation == "update" {
		knownBefore = make(map[string]struct{}, len(before.Items))
		for _, drama := range before.Items {
			knownBefore[drama.ID] = struct{}{}
		}
	}
	var headItems []nativeDrama
	defer func() {
		after := len(engine.nativeCached(source).Items)
		engine.changeSourceRecord(source, func(record *nativeSourceRecord) { record.Added = max(0, after-count) })
	}()
	if operation == "update" {
		if source == sourceHongguo {
			if err := engine.waitForPlaybackIdle(ctx, source, "查找新劇"); err != nil {
				return err
			}
		}
		engine.changeSourceRecord(source, func(record *nativeSourceRecord) { record.Stage = "查找新劇" })
		if err := engine.loadSourceCatalogPage(ctx, source, 1); err != nil {
			return err
		}
		if source == sourceHongguo {
			for _, drama := range engine.nativeCached(source).Items {
				if _, existed := knownBefore[drama.ID]; !existed {
					headItems = append(headItems, drama)
				}
			}
		}
	}
	if operation == "more" || operation == "update" {
		pageCtx := sourceHistoryPageContext(ctx, source)
		if source == sourceHongguo && operation == "update" {
			// Process one remote page per feed in each iteration, but continue
			// automatically until every Red Fruit history cursor is exhausted.
			for {
				if err := engine.waitForPlaybackIdle(ctx, source, "繼續載入歷史目錄"); err != nil {
					return err
				}
				current := engine.nativeCached(source)
				if !current.HasMore {
					break
				}
				nextPage := current.Page + 1
				engine.changeSourceRecord(source, func(record *nativeSourceRecord) {
					record.Stage = fmt.Sprintf("繼續載入歷史目錄（第 %d 頁）", nextPage)
				})
				if err := engine.loadMoreSourceCatalogPages(pageCtx, source, 1); err != nil {
					return err
				}
				if !engine.nativeCached(source).HasMore {
					break
				}
				if err := waitForSourceCatalogInterval(ctx); err != nil {
					return err
				}
			}
			engine.changeSourceRecord(source, func(record *nativeSourceRecord) {
				record.Stage = "歷史目錄載入完成"
			})
		} else {
			if source == sourceHongguo {
				if err := engine.waitForPlaybackIdle(ctx, source, "繼續載入歷史目錄"); err != nil {
					return err
				}
			}
			engine.changeSourceRecord(source, func(record *nativeSourceRecord) { record.Stage = "繼續載入歷史分頁" })
			limit := sourceUpdateHistoryPageLimit(source, operation, engine.downloader.cfg.MaxPagesPerSort)
			if err := engine.loadMoreSourceCatalogPages(pageCtx, source, limit); err != nil {
				return err
			}
		}
	}
	if operation == "more" {
		return nil
	}
	items := engine.nativeCached(source).Items
	if source == sourceHongguo && operation == "update" {
		// Enrich only newly discovered head entries during an update. The explicit
		// "補齊資料" action handles the full catalog; normal updates should not
		// trigger thousands of detail requests after paging through history.
		items = headItems
	}
	return engine.completeSourceMetadata(ctx, source, operation, items)
}


const sourceMetadataBatchSize = 8

// sourceDramaNeedsMetadata describes which catalog entries still need their
// details enriched. Red Fruit's "fill metadata" action is a missing-data repair,
// not a request to refetch every complete title.
func sourceDramaNeedsMetadata(source, operation string, drama nativeDrama) bool {
	source = canonicalProviderSource(source)
	if operation == "vipMetadata" {
		return drama.VIP == nil
	}
	if source == sourceHongguo {
		// The feed intentionally omits some optional synopsis/date fields for
		// many titles. They are not required for posters or playback, and
		// refetching every such title can create tens of thousands of requests.
		// Only enrich Red Fruit entries whose episode count is actually unknown.
		return drama.Episodes <= 0
	}
	return operation == "metadata" || operation == "vipMetadata" || drama.Episodes <= 0 || drama.Description == "" || source == sourceHuangdou && drama.VIP == nil
}

// sourceMetadataCandidates returns every eligible entry in cursor order. The
// caller limits each request batch; separating selection from batching keeps
// progress counts accurate and makes it harder to repeatedly select completed
// entries.
func sourceMetadataCandidates(items []nativeDrama, source, operation string, record nativeSourceRecord, now time.Time) []nativeDrama {
	source = canonicalProviderSource(source)
	start := 0
	for index, drama := range items {
		if drama.ID == record.MetadataCursor {
			start = index + 1
			break
		}
	}
	candidates := make([]nativeDrama, 0, len(items))
	for scanned := 0; scanned < len(items); scanned++ {
		drama := items[(start+scanned)%len(items)]
		if operation != "vipMetadata" && now.Sub(record.MetadataChecked[drama.ID]) < 24*time.Hour {
			continue
		}
		if now.Before(record.MetadataRetryAt[drama.ID]) {
			continue
		}
		if sourceDramaNeedsMetadata(source, operation, drama) {
			candidates = append(candidates, drama)
		}
	}
	return candidates
}

// completeSourceMetadata uses small bounded batches for every provider. For
// Red Fruit, it automatically continues with the next batch until no eligible
// entries remain or the task context expires; the durable cursor/checkpoint
// lets the next manual update resume after a timeout or failed request.
func (engine *nativeEngine) completeSourceMetadata(ctx context.Context, source, operation string, items []nativeDrama) error {
	source = canonicalProviderSource(source)
	autoContinue := source == sourceHongguo && operation == "metadata"
	engine.mu.Lock()
	initialRecord := engine.sourceRecords[source]
	initialCandidates := sourceMetadataCandidates(items, source, operation, initialRecord, time.Now())
	engine.mu.Unlock()

	total := len(initialCandidates)
	if !autoContinue && total > sourceMetadataBatchSize {
		total = sourceMetadataBatchSize
	}
	engine.changeSourceRecord(source, func(record *nativeSourceRecord) {
		record.Stage = "補齊劇集資料"
		record.Completed = 0
		record.Total = total
	})
	if total == 0 {
		return nil
	}

	processed := 0
	incomplete := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		engine.mu.Lock()
		record := engine.sourceRecords[source]
		candidates := sourceMetadataCandidates(items, source, operation, record, time.Now())
		engine.mu.Unlock()
		if len(candidates) == 0 {
			break
		}
		batchSize := min(sourceMetadataBatchSize, len(candidates))
		if !autoContinue {
			batchSize = min(batchSize, total-processed)
			if batchSize <= 0 {
				break
			}
		}
		pending := candidates[:batchSize]
		var failures []error
		for _, drama := range pending {
			if err := ctx.Err(); err != nil {
				return err
			}
			if source == sourceHongguo {
				if err := engine.waitForPlaybackIdle(ctx, source, "補齊劇集資料"); err != nil {
					return err
				}
			}
			result, err := engine.nativeDetail(ctx, drama)
			unknownVIP := false
			incompleteMetadata := false
			if err == nil {
				fresh := result.(map[string]any)["drama"].(nativeDrama)
				var metadataErr error
				if nativeNeedsExtraMetadata(fresh) {
					fresh, metadataErr = engine.nativeExtraMetadata(ctx, fresh)
				}
				unknownVIP = source == sourceHuangdou && fresh.VIP == nil
				incompleteMetadata = source == sourceHongguo && sourceDramaNeedsMetadata(source, operation, fresh)
				engine.mu.Lock()
				engine.catalogs[source] = mergeNativeCatalog(engine.catalogs[source], []nativeDrama{fresh})
				for key, catalogItems := range engine.catalogs {
					if !strings.HasPrefix(key, source+"|") {
						continue
					}
					for index := range catalogItems {
						if catalogItems[index].ID == fresh.ID {
							catalogItems[index] = mergeNativeDrama(catalogItems[index], fresh)
						}
					}
				}
				err = engine.writeCatalogDiskLocked()
				engine.mu.Unlock()
				if err == nil {
					err = metadataErr
				}
			}
			engine.changeSourceRecord(source, func(record *nativeSourceRecord) {
				record.MetadataCursor = drama.ID
				if err == nil {
					if incompleteMetadata {
						// The endpoint responded, but did not provide the missing
						// fields. Do not mark this title as checked for 24 hours.
						if record.MetadataRetryAt == nil {
							record.MetadataRetryAt = map[string]time.Time{}
						}
						record.MetadataRetryAt[drama.ID] = time.Now().Add(5 * time.Minute)
					} else {
						if record.MetadataChecked == nil {
							record.MetadataChecked = map[string]time.Time{}
						}
						record.MetadataChecked[drama.ID] = time.Now()
						delete(record.MetadataRetryAt, drama.ID)
					}
					if unknownVIP {
						if record.MetadataRetryAt == nil {
							record.MetadataRetryAt = map[string]time.Time{}
						}
						record.MetadataRetryAt[drama.ID] = time.Now().Add(5 * time.Minute)
					}
				} else if ctx.Err() == nil {
					if record.MetadataRetryAt == nil {
						record.MetadataRetryAt = map[string]time.Time{}
					}
					retry := 5 * time.Minute
					if strings.Contains(err.Error(), "404") {
						retry = 24 * time.Hour
					}
					record.MetadataRetryAt[drama.ID] = time.Now().Add(retry)
				}
			})
			processed++
			if incompleteMetadata && err == nil {
				incomplete++
			}
			engine.changeSourceRecord(source, func(record *nativeSourceRecord) {
				record.Completed = processed
				record.Total = total
				record.Stage = "補齊劇集資料"
			})
			if err != nil {
				var persistence *nativePersistenceError
				if errors.As(err, &persistence) {
					return err
				}
				failures = append(failures, err)
				var backoff *requestBackoff
				if errors.As(err, &backoff) || len(failures) >= 2 {
					break
				}
			}
		}
		if len(failures) > 0 {
			return errors.Join(failures...)
		}
		if !autoContinue {
			break
		}
	}
	if incomplete > 0 {
		return fmt.Errorf("%d 部紅果劇集資料仍不完整，已安排 5 分鐘後重試", incomplete)
	}
	return nil
}

func (engine *nativeEngine) loadSourceCatalogPage(ctx context.Context, source string, page int) error {
	result, err := engine.nativeCatalog(ctx, nativeInput{Source: source, Page: page, Force: true})
	if err != nil {
		return err
	}
	if result.Warning != "" {
		if result.saveError != nil {
			return result.saveError
		}
		return errors.New(result.Warning)
	}
	return nil
}

func (engine *nativeEngine) loadMoreSourceCatalogPages(ctx context.Context, source string, limit int) error {
	if limit <= 0 {
		return nil
	}
	for loaded := 0; loaded < limit; loaded++ {
		current := engine.nativeCached(source)
		if !current.HasMore {
			return nil
		}
		page := current.Page + 1
		if len(current.Items) == 0 {
			page = 1
		}
		if page > 1000000 {
			return errors.New("站源分页已达到接口范围，已保留目录位置")
		}
		if err := engine.loadSourceCatalogPage(ctx, source, page); err != nil {
			return err
		}
	}
	return nil
}
