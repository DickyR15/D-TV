package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"time"
)

type nativeCoverNetworkKey struct{}

type nativeCoverRepair struct {
	done            chan struct{}
	providerAddress string
	address         string
	err             error
	retryAt         time.Time
}

// load keeps a strict source-preference order:
// catalogue poster -> provider detail poster -> external poster.
func (cache *nativeCoverCache) load(ctx context.Context, drama nativeDrama, force bool) (string, error) {
	primaryAddress := strings.TrimSpace(drama.Cover)
	primaryErr := errors.New("站源尚未提供原始海報網址")
	if primaryAddress != "" {
		path, err := cache.loadAddress(ctx, drama, force)
		if err == nil || ctx.Err() != nil {
			return path, err
		}
		primaryErr = err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// The catalogue can supply several artwork fields. Keep the original first,
	// then try every other provider URL before another metadata or external lookup.
	for _, candidate := range nativeUniqueCoverAddresses(drama.CoverCandidates...) {
		if candidate == primaryAddress {
			continue
		}
		path, err := cache.loadAlternativeAddress(
			ctx, drama, candidate,
			nativeCoverRefererForDrama(cache.downloader, drama, candidate), "provider",
		)
		if err == nil {
			return path, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		force = true
	}

	cachedProvider, cachedExternal := cache.cachedRepair(drama.ID)
	providerAddress := firstNonEmpty(strings.TrimSpace(drama.ProviderFallbackCover), cachedProvider)
	if providerAddress != "" && providerAddress != primaryAddress {
		path, err := cache.loadAlternativeAddress(
			ctx, drama, providerAddress,
			nativeCoverFallbackReferer(cache.downloader, drama, providerAddress), "provider",
		)
		if err == nil {
			return path, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		force = true
	}

	// Refresh/check provider-native detail metadata before consulting an
	// external match. This protects provider priority even after a restart
	// when only an external fallback was persisted.
	providerAddress, repairedExternal, repairErr := cache.repair(ctx, drama, force, strings.TrimSpace(drama.FallbackCover) == "" && cachedExternal == "")
	if providerAddress != "" && providerAddress != primaryAddress {
		path, err := cache.loadAlternativeAddress(
			ctx, drama, providerAddress,
			nativeCoverFallbackReferer(cache.downloader, drama, providerAddress), "provider",
		)
		if err == nil {
			return path, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		force = true
	}

	externalCandidates := duoduoUniqueStrings([]string{
		strings.TrimSpace(drama.FallbackCover),
		cachedExternal,
		repairedExternal,
	})
	for _, externalAddress := range externalCandidates {
		if externalAddress == "" || externalAddress == primaryAddress || externalAddress == providerAddress {
			continue
		}
		path, err := cache.loadAlternativeAddress(
			ctx, drama, externalAddress,
			nativeCoverFallbackReferer(cache.downloader, drama, externalAddress), "external",
		)
		if err == nil {
			return path, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		force = true
	}

	if force && repairErr == nil {
		providerAddress, repairedExternal, repairErr = cache.repair(ctx, drama, true, true)
		if providerAddress != "" && providerAddress != primaryAddress {
			path, err := cache.loadAlternativeAddress(
				ctx, drama, providerAddress,
				nativeCoverFallbackReferer(cache.downloader, drama, providerAddress), "provider",
			)
			if err == nil {
				return path, nil
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
		}
		if repairedExternal != "" && repairedExternal != primaryAddress && repairedExternal != providerAddress {
			path, err := cache.loadAlternativeAddress(
				ctx, drama, repairedExternal,
				nativeCoverFallbackReferer(cache.downloader, drama, repairedExternal), "external",
			)
			if err == nil {
				return path, nil
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
		}
	}
	if repairErr != nil {
		return "", errors.Join(primaryErr, repairErr)
	}
	return "", primaryErr
}

func (cache *nativeCoverCache) cachedRepair(id string) (string, string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	repair := cache.repairs[id]
	if repair == nil || repair.retryAt.IsZero() {
		return "", ""
	}
	return repair.providerAddress, repair.address
}

func nativeCoverFallbackReferer(downloader *Downloader, drama nativeDrama, address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err == nil {
		host := strings.ToLower(parsed.Hostname())
		if host == "bgm.tv" || strings.HasSuffix(host, ".bgm.tv") {
			return "https://bgm.tv/"
		}
		if host == "movie.douban.com" || host == "www.douban.com" || strings.HasSuffix(host, ".doubanio.com") {
			return "https://movie.douban.com/"
		}
	}
	return nativeCoverRefererForDrama(downloader, drama, address)
}

// loadAlternativeAddress validates and caches a non-catalogue poster using a
// cache namespace derived from its origin. Provider and external images cannot
// overwrite each other or the original catalogue poster cache.
func (cache *nativeCoverCache) loadAlternativeAddress(
	ctx context.Context,
	drama nativeDrama,
	address string,
	referer string,
	cacheClass string,
) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || !validNativeCoverURL(parsed) {
		return "", errors.New("備用海報網址無效")
	}
	source := canonicalProviderSource(drama.Source)
	if source == "" {
		source = sourceFromDramaID(drama.ID)
	}
	if referer == "" {
		referer = nativeCoverRefererForDrama(cache.downloader, drama, address)
	}
	digest := sha256.Sum256([]byte(source + "\x00" + cacheClass + "\x00" + address + "\x00" + referer))
	key := hex.EncodeToString(digest[:])
	path := filepathJoinCover(cache.directory, key+".img")

	cache.mu.Lock()
	var stalePath string
	if entry, found := cache.entries[key]; found {
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() && info.Size() == entry.size {
			entry.usedAt = time.Now()
			cache.entries[key] = entry
			age := time.Since(entry.updatedAt)
			if age >= 0 && age < nativeCoverTTL {
				cache.mu.Unlock()
				return path, nil
			}
			stalePath = path
		} else {
			delete(cache.entries, key)
			cache.size -= entry.size
		}
	}
	cache.mu.Unlock()

	select {
	case cache.slots <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	var data []byte
	var downloadErr error
	attempts := []duoduoPosterAttempt{{address: address, referer: referer}}
	// Only provider-hosted artwork needs Duoduo's page Referer and its
	// provider-specific CDN/Baidu proxy retries. External database images must
	// retain their own trusted Referer (e.g. Douban's domain) or their CDN may
	// reject the download even though the poster URL is valid.
	if source == sourceDuoduo && cacheClass == "provider" {
		attempts = duoduoPosterAttemptsForDrama(address, cache.downloader, drama)
	}
	for _, attempt := range attempts {
		if ctx.Err() != nil {
			downloadErr = ctx.Err()
			break
		}
		data, downloadErr = cache.download(ctx, attempt.address, attempt.referer)
		if downloadErr == nil {
			break
		}
	}
	<-cache.slots
	if downloadErr != nil {
		if stalePath != "" {
			if _, statErr := os.Stat(stalePath); statErr == nil {
				return stalePath, nil
			}
		}
		return "", downloadErr
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err := writeNativeCacheFile(path, data); err != nil {
		return "", err
	}
	cache.size -= cache.entries[key].size
	now := time.Now()
	cache.entries[key] = nativeCoverEntry{size: int64(len(data)), updatedAt: now, usedAt: now}
	cache.size += int64(len(data))
	cache.prune(key)
	return path, nil
}

func filepathJoinCover(directory, name string) string {
	return strings.TrimRight(directory, string(os.PathSeparator)) + string(os.PathSeparator) + name
}

type nativeCoverExternalPosterResult struct {
	address string
	err     error
}

// startNativeCoverExternalPosterLookup overlaps the known exact Douban lookup
// with the provider-detail request. The provider poster is still downloaded
// and selected first; this only removes a serial metadata round trip.
func startNativeCoverExternalPosterLookup(
	ctx context.Context,
	downloader *Downloader,
	drama nativeDrama,
) <-chan nativeCoverExternalPosterResult {
	if downloader == nil || !validDoubanSubjectID(drama.DoubanID) {
		return nil
	}
	result := make(chan nativeCoverExternalPosterResult, 1)
	go func() {
		address, err := downloader.externalPosterAddress(ctx, drama)
		result <- nativeCoverExternalPosterResult{address: address, err: err}
	}()
	return result
}

func (cache *nativeCoverCache) repair(ctx context.Context, drama nativeDrama, force bool, searchExternal bool) (string, string, error) {
	cache.mu.Lock()
	if previous := cache.repairs[drama.ID]; previous != nil {
		if previous.retryAt.IsZero() {
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", "", ctx.Err()
			case <-previous.done:
				return previous.providerAddress, previous.address, previous.err
			}
		}
		if !force && time.Now().Before(previous.retryAt) {
			cache.mu.Unlock()
			return previous.providerAddress, previous.address, previous.err
		}
	}
	if len(cache.repairs) >= 512 {
		for id, entry := range cache.repairs {
			if !entry.retryAt.IsZero() {
				delete(cache.repairs, id)
				break
			}
		}
		if len(cache.repairs) >= 512 {
			cache.mu.Unlock()
			return "", "", errors.New("海報正在載入，請稍後重試")
		}
	}
	call := &nativeCoverRepair{done: make(chan struct{})}
	cache.repairs[drama.ID] = call
	cache.mu.Unlock()

	var providerAddress, externalAddress string
	var providerErr, externalErr error
	select {
	case cache.repairSlots <- struct{}{}:
		// Queue time must not consume the per-title repair timeout. Previously,
		// queued titles could expire before their metadata lookup even began.
		func() {
			defer func() { <-cache.repairSlots }()
			work, cancel := context.WithTimeout(ctx, 22*time.Second)
			defer cancel()

			// When the catalogue already contains a valid exact Douban ID,
			// resolve its poster metadata while the official provider detail
			// request is in flight. Provider artwork remains higher priority.
			externalLookup := (<-chan nativeCoverExternalPosterResult)(nil)
			if searchExternal {
				externalLookup = startNativeCoverExternalPosterLookup(work, cache.downloader, drama)
			}

			nativeAddresses, doubanID, nativeErr := cache.downloader.nativeCoverRecoveryMetadata(work, drama)
			if drama.DoubanID == "" {
				drama.DoubanID = doubanID
			}
			if nativeErr != nil {
				providerErr = nativeErr
			}
			for _, nativeAddress := range nativeUniqueCoverAddresses(nativeAddresses...) {
				if nativeAddress == drama.Cover {
					continue
				}
				_, candidateErr := cache.loadAlternativeAddress(
					work, drama, nativeAddress,
					nativeCoverFallbackReferer(cache.downloader, drama, nativeAddress), "provider",
				)
				if candidateErr == nil {
					providerAddress = nativeAddress
					break
				}
				providerErr = candidateErr
				if work.Err() != nil {
					break
				}
			}
			if providerAddress == "" && providerErr == nil {
				providerErr = errors.New("站源詳情沒有可用的備用海報")
			}

			// An official/provider detail poster always outranks external matches.
			if providerAddress == "" && work.Err() == nil && searchExternal {
				if externalLookup != nil {
					select {
					case result := <-externalLookup:
						externalAddress, externalErr = result.address, result.err
					case <-work.Done():
						externalErr = work.Err()
					}
				} else {
					// No exact Douban ID was present in the catalogue. This path
					// must wait for provider detail metadata, which may supply one.
					externalAddress, externalErr = cache.downloader.externalPosterAddress(work, drama)
				}
				if externalErr == nil && externalAddress != "" && externalAddress != drama.Cover {
					_, candidateErr := cache.loadAlternativeAddress(work, drama, externalAddress, nativeCoverFallbackReferer(cache.downloader, drama, externalAddress), "external")
					if candidateErr != nil {
						externalErr = candidateErr
						externalAddress = ""
					}
				}
			}
		}()
	case <-ctx.Done():
		providerErr = ctx.Err()
	}

	err := error(nil)
	if providerAddress == "" && externalAddress == "" {
		err = errors.Join(providerErr, externalErr)
		if err == nil {
			err = errors.New("沒有找到可驗證的備用海報")
		}
	}
	cache.mu.Lock()
	call.providerAddress, call.address, call.err = providerAddress, externalAddress, err
	call.retryAt = time.Now().Add(5 * time.Minute)
	close(call.done)
	cache.mu.Unlock()
	return providerAddress, externalAddress, err
}

// nativeMergeCoverRepairMetadata restores identifiers and matching hints that
// may be absent from older Flutter models or persisted watch-history entries.
// These fields are needed to look up a safe external poster when the source CDN
// has removed its original image.
func nativeMergeCoverRepairMetadata(drama, cached nativeDrama) nativeDrama {
	if drama.Source == "" {
		drama.Source = cached.Source
	}
	if drama.SourceID == "" {
		drama.SourceID = cached.SourceID
	}
	if drama.DoubanID == "" {
		drama.DoubanID = cached.DoubanID
	}
	if drama.Title == "" || drama.Title == "短劇" {
		drama.Title = cached.Title
	}
	if drama.Category == "" {
		drama.Category = cached.Category
	}
	if drama.OnlineDate == "" {
		drama.OnlineDate = cached.OnlineDate
	}
	if len(drama.Tags) == 0 {
		drama.Tags = append([]string(nil), cached.Tags...)
	}
	return drama
}

func (engine *nativeEngine) loadCover(ctx context.Context, drama nativeDrama, force bool) (map[string]any, error) {
	engine.mu.Lock()
	for _, cached := range engine.catalogs[drama.Source] {
		if cached.ID != drama.ID {
			continue
		}
		drama = nativeMergeCoverRepairMetadata(drama, cached)
		if drama.Cover == "" {
			drama.Cover = cached.Cover
		}
		if drama.ProviderFallbackCover == "" {
			drama.ProviderFallbackCover = cached.ProviderFallbackCover
		}
		if drama.FallbackCover == "" {
			drama.FallbackCover = cached.FallbackCover
		}
		drama.CoverCandidates = nativeUniqueCoverAddresses(append(drama.CoverCandidates, cached.CoverCandidates...)...)
		break
	}
	if cached, ok := engine.detailCache[drama.ID]; ok {
		drama = nativeMergeCoverRepairMetadata(drama, cached.Drama)
		if drama.Cover == "" {
			drama.Cover = cached.Drama.Cover
		}
		if drama.ProviderFallbackCover == "" {
			drama.ProviderFallbackCover = cached.Drama.ProviderFallbackCover
		}
		if drama.FallbackCover == "" {
			drama.FallbackCover = cached.Drama.FallbackCover
		}
		drama.CoverCandidates = nativeUniqueCoverAddresses(append(drama.CoverCandidates, cached.Drama.CoverCandidates...)...)
	}
	engine.mu.Unlock()

	path, err := engine.covers.load(ctx, drama, force)
	if err != nil {
		return nil, err
	}
	providerAddress, externalAddress := engine.covers.cachedRepair(drama.ID)
	if providerAddress != "" && providerAddress != drama.Cover {
		engine.saveProviderFallbackCover(drama.ID, providerAddress)
	}
	if externalAddress != "" && externalAddress != drama.Cover {
		engine.saveFallbackCover(drama.ID, externalAddress)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	header := make([]byte, 256)
	count, err := file.Read(header)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return map[string]any{"path": path, "heic": isHEICImage(header[:count])}, nil
}

// Provider-native detail artwork is kept separately so it always outranks an
// external database match while leaving the original catalogue URL untouched.
func (engine *nativeEngine) saveProviderFallbackCover(id, address string) {
	if strings.TrimSpace(address) == "" {
		return
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	changed := false
	for _, items := range engine.catalogs {
		for index := range items {
			if items[index].ID == id && items[index].ProviderFallbackCover != address {
				items[index].ProviderFallbackCover = address
				changed = true
			}
		}
	}
	for key, entry := range engine.detailCache {
		if entry.Drama.ID == id && entry.Drama.ProviderFallbackCover != address {
			entry.Drama.ProviderFallbackCover = address
			engine.detailCache[key] = entry
		}
	}
	if changed {
		engine.writeCatalogDiskLocked()
	}
}

// External matches are cached independently from both catalogue and provider
// detail artwork. A refreshed upstream poster can therefore regain priority.
func (engine *nativeEngine) saveFallbackCover(id, address string) {
	if strings.TrimSpace(address) == "" {
		return
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	changed := false
	for _, items := range engine.catalogs {
		for index := range items {
			if items[index].ID == id && items[index].FallbackCover != address {
				items[index].FallbackCover = address
				changed = true
			}
		}
	}
	for key, entry := range engine.detailCache {
		if entry.Drama.ID == id && entry.Drama.FallbackCover != address {
			entry.Drama.FallbackCover = address
			engine.detailCache[key] = entry
		}
	}
	if changed {
		engine.writeCatalogDiskLocked()
	}
}

func (engine *nativeEngine) prepareCover(ctx context.Context, drama nativeDrama) (any, error) {
	result, err := engine.loadCover(ctx, drama, false)
	if err != nil {
		return nil, err
	}
	if result["heic"] != true {
		return result, nil
	}
	file, err := os.Open(result["path"].(string))
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, nativeCoverMaxBytes+1))
	file.Close()
	if err != nil {
		return nil, err
	}
	image, err := extractHEICImage(data)
	if err != nil {
		return nil, err
	}
	output, err := os.CreateTemp(engine.covers.directory, ".decode-*.hevc")
	if err != nil {
		return nil, err
	}
	_, writeErr := output.Write(image.data)
	closeErr := output.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(output.Name())
		return nil, errors.Join(writeErr, closeErr)
	}
	result["input"], result["filters"] = output.Name(), strings.Join(image.filters, ",")
	return result, nil
}
