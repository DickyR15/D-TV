package core

import (
	"errors"
	"strings"
)

const nativeCatalogMaxBytes = 32 << 20
const nativeSourceMaxBytes = 4 << 20

var errNativeCatalogLimit = errors.New("片庫超過本機儲存上限（32 MiB），新內容尚未儲存，已暫停繼續載入")
var errNativeSourceLimit = errors.New("片源任務紀錄超過本機儲存上限（4 MiB），最新紀錄尚未儲存")

type nativePersistenceError struct {
	label string
	cause error
}

func (err *nativePersistenceError) Error() string {
	if errors.Is(err.cause, errNativeCatalogLimit) || errors.Is(err.cause, errNativeSourceLimit) {
		return err.cause.Error()
	}
	return err.label + "尚未儲存，最新結果僅在本次執行期間保留；請檢查剩餘空間與儲存權限後重試"
}

func (err *nativePersistenceError) Unwrap() error { return err.cause }

func nativeSaveError(label string, err error) error {
	if err == nil {
		return nil
	}
	return &nativePersistenceError{label: label, cause: err}
}

func joinNativeWarnings(warnings ...string) string {
	seen := map[string]bool{}
	var values []string
	for _, warning := range warnings {
		if warning != "" && !seen[warning] {
			seen[warning] = true
			values = append(values, warning)
		}
	}
	return strings.Join(values, "；")
}

func (engine *nativeEngine) retryCatalogSave() (bool, error) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.catalogSaveError == nil {
		return false, nil
	}
	return true, engine.writeCatalogDiskLocked()
}

func (engine *nativeEngine) finishCatalogSaveLocked() {
	changed := false
	for source, record := range engine.sourceRecords {
		if record.NeedsSave {
			record.NeedsSave = false
			if !record.Running {
				record.Stage = "已保存，可继续更新"
			}
			engine.sourceRecords[source] = record
			changed = true
		}
	}
	if changed {
		engine.saveSourceRecordsLocked()
	}
}

func (engine *nativeEngine) markCatalogSavePendingLocked() {
	if engine.sourceRecords == nil {
		engine.sourceRecords = map[string]nativeSourceRecord{}
	}
	changed := false
	for key := range engine.catalogs {
		source, _, _ := strings.Cut(key, "|")
		record := engine.sourceRecords[source]
		if !isHuangguoProviderSource(source) || record.NeedsSave {
			continue
		}
		record.NeedsSave = true
		if !record.Running {
			record.Stage = "等待保存"
		}
		engine.sourceRecords[source] = record
		changed = true
	}
	if changed {
		engine.saveSourceRecordsLocked()
	}
}

func (engine *nativeEngine) storageWarningLocked() string {
	var warnings []string
	for _, err := range []error{engine.catalogSaveError, engine.sourceSaveError} {
		if err != nil {
			warnings = append(warnings, err.Error())
		}
	}
	return joinNativeWarnings(warnings...)
}

func (engine *nativeEngine) retrySourceSave(source string) nativeSourceStatus {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.catalogSaveError != nil {
		engine.writeCatalogDiskLocked()
	}
	if engine.sourceSaveError != nil {
		engine.saveSourceRecordsLocked()
	}
	return engine.sourceStatusLocked(source)
}
