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
	done    chan struct{}
	address string
	err     error
	retryAt time.Time
}

// load always gives the source-provided poster first refusal. A verified
// fallback is consulted only after the original URL and provider-native
// alternatives have failed, and is stored separately from Drama.Cover.
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

	fallbackAddress := strings.TrimSpace(drama.FallbackCover)
	if fallbackAddress == "" {
		cache.mu.Lock()
		if repair := cache.repairs[drama.ID]; repair != nil {
			fallbackAddress = repair.address
		}
		cache.mu.Unlock()
	}
	if fallbackAddress != "" && fallbackAddress != primaryAddress {
		path, err := cache.loadAlternativeAddress(
			ctx, drama, fallbackAddress, nativeCoverFallbackReferer(cache.downloader, drama, fallbackAddress),
		)
		if err == nil {
			return path, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// A previously verified image may have expired or disappeared. Bypass
		// the negative/positive repair cooldown and search for a new candidate.
		force = true
	}

	address, repairErr := cache.repair(ctx, drama, force)
	if repairErr != nil || address == "" || address == primaryAddress || address == fallbackAddress {
		return "", primaryErr
	}
	return cache.loadAlternativeAddress(
		ctx, drama, address, nativeCoverFallbackReferer(cache.downloader, drama, address),
	)
}

func nativeCoverFallbackReferer(downloader *Downloader, drama nativeDrama, address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err == nil {
		host := strings.ToLower(parsed.Hostname())
		if host == "bgm.tv" || strings.HasSuffix(host, ".bgm.tv") {
			return "https://bgm.tv/"
		}
	}
	source := canonicalProviderSource(drama.Source)
	if source == "" {
		source = sourceFromDramaID(drama.ID)
	}
	return nativeCoverReferer(downloader, source, address)
}

// loadAlternativeAddress downloads and validates a candidate without invoking
// the source-detail retry path again. It uses a separate cache key so an
// externally sourced image can never replace the source's original cache.
func (cache *nativeCoverCache) loadAlternativeAddress(
	ctx context.Context,
	drama nativeDrama,
	address string,
	referer string,
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
		referer = nativeCoverReferer(cache.downloader, source, address)
	}
	digest := sha256.Sum256([]byte(source + "\x00fallback\x00" + address + "\x00" + referer))
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
	if source == sourceDuoduo {
		attempts = duoduoPosterAttempts(address, referer)
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

func (cache *nativeCoverCache) repair(ctx context.Context, drama nativeDrama, force bool) (string, error) {
	cache.mu.Lock()
	if previous := cache.repairs[drama.ID]; previous != nil {
		if previous.retryAt.IsZero() {
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-previous.done:
				return previous.address, previous.err
			}
		}
		if !force && time.Now().Before(previous.retryAt) {
			cache.mu.Unlock()
			return previous.address, previous.err
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
			return "", errors.New("海報正在載入，請稍後重試")
		}
	}
	call := &nativeCoverRepair{done: make(chan struct{})}
	cache.repairs[drama.ID] = call
	cache.mu.Unlock()

	work, cancel := context.WithTimeout(ctx, 22*time.Second)
	defer cancel()
	var address string
	var err error
	select {
	case cache.repairSlots <- struct{}{}:
		nativeAddress, nativeErr := cache.downloader.nativeCoverAddress(work, drama)
		if nativeErr == nil && nativeAddress != "" && nativeAddress != drama.Cover {
			_, candidateErr := cache.loadAlternativeAddress(
				work, drama, nativeAddress, nativeCoverReferer(cache.downloader, canonicalProviderSource(drama.Source), nativeAddress),
			)
			if candidateErr == nil {
				address = nativeAddress
			} else {
				nativeErr = candidateErr
			}
		}
		if address == "" && work.Err() == nil {
			externalAddress, externalErr := cache.downloader.bangumiPosterAddress(work, drama)
			if externalErr == nil && externalAddress != "" && externalAddress != drama.Cover {
				_, candidateErr := cache.loadAlternativeAddress(work, drama, externalAddress, "https://bgm.tv/")
				if candidateErr == nil {
					address = externalAddress
				} else {
					externalErr = candidateErr
				}
			}
			if address == "" {
				err = errors.Join(nativeErr, externalErr)
				if err == nil {
					err = errors.New("沒有找到可驗證的備用海報")
				}
			}
		} else if address == "" {
			err = nativeErr
		}
		<-cache.repairSlots
	case <-work.Done():
		err = work.Err()
	}

	cache.mu.Lock()
	call.address, call.err, call.retryAt = address, err, time.Now().Add(5*time.Minute)
	close(call.done)
	cache.mu.Unlock()
	return address, err
}

func (engine *nativeEngine) loadCover(ctx context.Context, drama nativeDrama, force bool) (map[string]any, error) {
	engine.mu.Lock()
	for _, cached := range engine.catalogs[drama.Source] {
		if cached.ID != drama.ID {
			continue
		}
		if drama.Cover == "" {
			drama.Cover = cached.Cover
		}
		if drama.FallbackCover == "" {
			drama.FallbackCover = cached.FallbackCover
		}
		break
	}
	if cached, ok := engine.detailCache[drama.ID]; ok {
		if drama.Cover == "" {
			drama.Cover = cached.Drama.Cover
		}
		if drama.FallbackCover == "" {
			drama.FallbackCover = cached.Drama.FallbackCover
		}
	}
	engine.mu.Unlock()

	path, err := engine.covers.load(ctx, drama, force)
	if err != nil {
		return nil, err
	}
	engine.covers.mu.Lock()
	address := ""
	if entry := engine.covers.repairs[drama.ID]; entry != nil {
		address = entry.address
	}
	engine.covers.mu.Unlock()
	if address != "" && address != drama.Cover {
		engine.saveFallbackCover(drama.ID, address)
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

// A repaired image is metadata alongside the upstream cover, never a
// replacement for it. A future healthy upstream poster therefore regains
// priority automatically without losing the verified fallback.
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
