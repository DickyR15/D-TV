package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const nativeCoverMaxBytes = 20 << 20
const nativeCoverCacheBytes = 256 << 20
const nativeCoverCacheEntries = 2000
const nativeCoverTTL = 30 * 24 * time.Hour
const nativeCoverRepairConcurrency = 3

type nativeCoverEntry struct {
	size      int64
	updatedAt time.Time
	usedAt    time.Time
}

type nativeCoverCall struct {
	done chan struct{}
	path string
	err  error
}

type nativeCoverCache struct {
	directory   string
	downloader  *Downloader
	mu          sync.Mutex
	entries     map[string]nativeCoverEntry
	pending     map[string]*nativeCoverCall
	slots       chan struct{}
	repairSlots chan struct{}
	repairs     map[string]*nativeCoverRepair
	size        int64
	limit       int64
}

func newNativeCoverCache(directory string, downloader *Downloader) *nativeCoverCache {
	cache := &nativeCoverCache{
		directory: filepath.Join(directory, "covers-v1"), downloader: downloader,
		entries: map[string]nativeCoverEntry{}, pending: map[string]*nativeCoverCall{},
		slots: make(chan struct{}, 4), limit: nativeCoverCacheBytes,
		repairSlots: make(chan struct{}, nativeCoverRepairConcurrency), repairs: map[string]*nativeCoverRepair{},
	}
	files, _ := os.ReadDir(cache.directory)
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".decode-") && !file.IsDir() {
			if info, err := file.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
				_ = os.Remove(filepath.Join(cache.directory, file.Name()))
			}
		}
		key, valid := strings.CutSuffix(file.Name(), ".img")
		if !valid || len(key) != 64 || file.IsDir() {
			continue
		}
		if _, err := hex.DecodeString(key); err != nil {
			continue
		}
		info, err := file.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > nativeCoverMaxBytes {
			continue
		}
		cache.entries[key] = nativeCoverEntry{size: info.Size(), updatedAt: info.ModTime(), usedAt: info.ModTime()}
		cache.size += info.Size()
	}
	cache.prune("")
	return cache
}

func nativeCoverReferer(downloader *Downloader, source, address string) string {
	if source == sourceCloudFront {
		if parsed, err := url.Parse(address); err == nil && (strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".zdmhyg.cn") || strings.EqualFold(parsed.Hostname(), "pic.tuafjz.cn")) {
			return downloader.providerBaseURL(sourceHuangguoAI) + "/"
		}
	}
	if source == sourceHuangdou {
		if parsed, err := url.Parse(address); err == nil && (strings.EqualFold(parsed.Hostname(), "tideember.cc") || strings.EqualFold(parsed.Hostname(), "xqjurgek.top")) {
			return parsed.Scheme + "://" + parsed.Host + "/home"
		}
		downloader.providerMu.Lock()
		host := downloader.providerHosts[source]
		downloader.providerMu.Unlock()
		if host == "" {
			host = downloader.providerBaseURL(source)
		}
		return host + "/home"
	}
	return downloader.providerBaseURL(source) + "/"
}

// Duoduo's image server can require the exact detail page as its Referer.
// The website shows posters at /play/{id}; a homepage-only Referer may be
// rejected even though the same image is visible from the official page.
func nativeCoverRefererForDrama(downloader *Downloader, drama nativeDrama, address string) string {
	source := canonicalProviderSource(drama.Source)
	if source == "" {
		source = sourceFromDramaID(drama.ID)
	}
	if source == sourceDuoduo {
		if _, sourceID, valid := splitProviderDramaID(drama.ID); valid && webProviderNumericID.MatchString(sourceID) {
			base := strings.TrimRight(downloader.providerBaseURL(source), "/")
			if base == "" {
				base = strings.TrimRight(duoduoPublicEntryURL, "/")
			}
			return base + "/play/" + url.PathEscape(sourceID)
		}
	}
	return nativeCoverReferer(downloader, source, address)
}

type duoduoPosterAttempt struct {
	address string
	referer string
}

// Baidu's gimg endpoint sometimes wraps a real poster URL inside its path,
// for example /gimg/app=2001&fmt=jpeg&src=https%3A%2F%2Fi0.hdslb.com%2F...png.
// Keep the original wrapper as the first choice, but expose the embedded
// source as a separate fallback so a broken proxy does not hide the real file.
func duoduoBaiduPosterSourceURL(address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || !validNativeCoverURL(parsed) {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.HasPrefix(host, "gimg") || !strings.HasSuffix(host, ".baidu.com") {
		return ""
	}
	values := url.Values{}
	if parsed.RawQuery != "" {
		queryValues, _ := url.ParseQuery(parsed.RawQuery)
		for key, entries := range queryValues {
			for _, value := range entries {
				values.Add(key, value)
			}
		}
	}
	pathValues, pathErr := url.ParseQuery(strings.TrimPrefix(parsed.Path, "/gimg/"))
	if pathErr == nil {
		for key, entries := range pathValues {
			for _, value := range entries {
				values.Add(key, value)
			}
		}
	}
	if values == nil {
		return ""
	}
	candidate := strings.TrimSpace(values.Get("src"))
	if strings.HasPrefix(candidate, "//") {
		candidate = "https:" + candidate
	} else if candidate != "" && !strings.Contains(candidate, "://") {
		// Baidu's gimg wrapper commonly stores a bare hostname in src,
		// e.g. src=img.example.com/path/poster.webp.
		candidate = "https://" + strings.TrimLeft(candidate, "/")
	}
	source, err := url.Parse(candidate)
	if err != nil || !validNativeCoverURL(source) || strings.EqualFold(source.Hostname(), host) {
		return ""
	}
	return source.String()
}

// Only public DNS hostnames are sent to the third-party image proxy. This
// avoids turning poster fallback into a proxy for localhost or literal IPs.
// The full escaped path and query are retained because poster URLs can be
// signed or can themselves be image-proxy wrappers.
func duoduoPosterProxyURL(address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || !validNativeCoverURL(parsed) {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" || !strings.Contains(host, ".") || strings.Contains(host, "..") ||
		net.ParseIP(host) != nil || host == "localhost" ||
		strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "80" && port != "443" {
		return ""
	}
	parsed.Fragment = ""
	target := strings.TrimPrefix(parsed.String(), parsed.Scheme+"://")
	return "https://images.weserv.nl/?url=" + url.QueryEscape(target)
}

func duoduoPosterAttemptsForDrama(address string, downloader *Downloader, drama nativeDrama) []duoduoPosterAttempt {
	referer := nativeCoverRefererForDrama(downloader, drama, address)
	attempts := duoduoPosterAttempts(address, referer)

	// Keep the former root-page Referer as a compatibility retry for image
	// mirrors that only allow the site's origin-level hotlink policy.
	source := canonicalProviderSource(drama.Source)
	if source == "" {
		source = sourceFromDramaID(drama.ID)
	}
	rootReferer := nativeCoverReferer(downloader, source, address)
	if rootReferer != "" && rootReferer != referer {
		attempts = append(attempts, duoduoPosterAttempts(address, rootReferer)... )
	}

	seen := make(map[string]bool, len(attempts))
	unique := make([]duoduoPosterAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		key := attempt.address + "\x00" + attempt.referer
		if attempt.address == "" || seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, attempt)
	}
	return unique
}
func duoduoPosterAttempts(address, referer string) []duoduoPosterAttempt {
	var attempts []duoduoPosterAttempt
	seen := map[string]bool{}
	add := func(candidate, candidateReferer string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		identity := candidate + "\x00" + candidateReferer
		if seen[identity] {
			return
		}
		seen[identity] = true
		attempts = append(attempts, duoduoPosterAttempt{
			address: candidate,
			referer: firstNonEmpty(candidateReferer, referer),
		})
	}
	add(address, referer)
	if sourceURL := duoduoBaiduPosterSourceURL(address); sourceURL != "" {
		add(address, "https://www.baidu.com/")
		add(sourceURL, "https://www.bilibili.com/")
		add(sourceURL, "https://www.baidu.com/")
	}
	if proxyURL := duoduoPosterProxyURL(address); proxyURL != "" {
		add(proxyURL, "https://images.weserv.nl/")
	}
	if sourceURL := duoduoBaiduPosterSourceURL(address); sourceURL != "" {
		if proxyURL := duoduoPosterProxyURL(sourceURL); proxyURL != "" {
			add(proxyURL, "https://images.weserv.nl/")
		}
	}
	return attempts
}

func validNativeCoverURL(address *url.URL) bool {
	return address != nil && (address.Scheme == "https" || address.Scheme == "http") && address.Hostname() != "" && address.User == nil
}

func (cache *nativeCoverCache) loadAddress(ctx context.Context, drama nativeDrama, force bool) (string, error) {
	drama.Cover = repairLegacyCoverURL(drama)
	address, err := url.Parse(drama.Cover)
	if err != nil || !validNativeCoverURL(address) {
		return "", errors.New("海报地址无效")
	}
	source := canonicalProviderSource(drama.Source)
	if source == "" {
		source = sourceFromDramaID(drama.ID)
	}
	referer := nativeCoverRefererForDrama(cache.downloader, drama, drama.Cover)
	digest := sha256.Sum256([]byte(source + "\x00" + drama.Cover + "\x00" + referer))
	key := hex.EncodeToString(digest[:])
	path := filepath.Join(cache.directory, key+".img")
	cache.mu.Lock()
	fallback := ""
	if entry, found := cache.entries[key]; found {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() == entry.size {
			entry.usedAt = time.Now()
			cache.entries[key] = entry
			age := time.Since(entry.updatedAt)
			if !force && age >= 0 && age < nativeCoverTTL {
				cache.mu.Unlock()
				return path, nil
			}
			if !force {
				fallback = path
			}
		} else {
			delete(cache.entries, key)
			cache.size -= entry.size
		}
	}
	call := cache.pending[key]
	if call == nil {
		if len(cache.pending) >= 256 {
			cache.mu.Unlock()
			return "", errors.New("海报正在加载，请稍后重试")
		}
		call = &nativeCoverCall{done: make(chan struct{})}
		cache.pending[key] = call
		go cache.fetch(key, drama, referer, fallback, call)
	}
	cache.mu.Unlock()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-call.done:
		return call.path, call.err
	}
}

func (cache *nativeCoverCache) fetch(key string, drama nativeDrama, referer, fallback string, call *nativeCoverCall) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var data []byte
	var err error
	select {
	case cache.slots <- struct{}{}:
		coverSource := canonicalProviderSource(drama.Source)
		if coverSource == "" {
			coverSource = sourceFromDramaID(drama.ID)
		}
		attempts := []duoduoPosterAttempt{{address: drama.Cover, referer: referer}}
		if coverSource == sourceDuoduo {
			attempts = duoduoPosterAttemptsForDrama(drama.Cover, cache.downloader, drama)
		}
		tryAttempts := func(candidates []duoduoPosterAttempt) {
			for _, attempt := range candidates {
				if ctx.Err() != nil {
					err = ctx.Err()
					return
				}
				data, err = cache.download(ctx, attempt.address, attempt.referer)
				if err == nil {
					return
				}
			}
		}
		tryAttempts(attempts)

		// If the preferred catalogue poster is dead, fetch the detail metadata
		// once and try every distinct thumbnail/slide/poster field it supplies.
		// This is a last resort only; normal image loads still use one request.
		if err != nil && ctx.Err() == nil && coverSource == sourceDuoduo {
			_, sourceID, valid := splitProviderDramaID(drama.ID)
			if valid {
				_, row, _, _, detailErr := cache.downloader.fetchDuoduoDetailRow(ctx, sourceID)
				if detailErr == nil {
					for _, alternative := range duoduoPosterAddressesFromRow(row) {
						if alternative == drama.Cover || ctx.Err() != nil {
							continue
						}
						tryAttempts(duoduoPosterAttemptsForDrama(alternative, cache.downloader, drama))
						if err == nil {
							break
						}
					}
				}
			}
		}
		<-cache.slots
	case <-ctx.Done():
		err = ctx.Err()
	}
	path := filepath.Join(cache.directory, key+".img")
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err == nil {
		err = writeNativeCacheFile(path, data)
		if err == nil {
			cache.size -= cache.entries[key].size
			now := time.Now()
			cache.entries[key] = nativeCoverEntry{size: int64(len(data)), updatedAt: now, usedAt: now}
			cache.size += int64(len(data))
			cache.prune(key)
			call.path = path
		}
	}
	if err != nil && fallback != "" {
		if _, statErr := os.Stat(fallback); statErr == nil {
			call.path, err = fallback, nil
		}
	}
	call.err = err
	delete(cache.pending, key)
	close(call.done)
	if err != nil {
		cache.downloader.recordDiagnostic(diagnosticEvent{Event: "cover_failed", Source: drama.Source, DramaID: drama.ID, Message: err.Error()})
	}
}

func (cache *nativeCoverCache) download(ctx context.Context, address, referer string) ([]byte, error) {
	request, err := http.NewRequestWithContext(context.WithValue(ctx, nativeCoverNetworkKey{}, true), http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Referer", referer)
	request.Header.Set("Accept", "image/webp,image/jpeg,image/png,image/gif,*/*;q=0.5")
	request.Header.Set("Sec-Fetch-Mode", "no-cors")
	request.Header.Set("Sec-Fetch-Dest", "image")
	client := *cache.downloader.client
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !validNativeCoverURL(request.URL) {
			return errors.New("海报重定向地址无效")
		}
		request.Header.Set("Referer", referer)
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if !nativeCoverResponseComplete(response) {
		return nil, fmt.Errorf("海報請求未取得完整圖片：HTTP %d", response.StatusCode)
	}
	if response.ContentLength > nativeCoverMaxBytes {
		return nil, errors.New("海报文件过大")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, nativeCoverMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > nativeCoverMaxBytes {
		return nil, errors.New("海报文件过大")
	}
	data = nativeDecodeCover(data)
	if !nativeIsCoverImage(data) {
		return nil, errors.New("站源未返回有效海报")
	}
	return data, nil
}

func nativeCoverResponseComplete(response *http.Response) bool {
	if response == nil {
		return false
	}
	if response.StatusCode == http.StatusOK {
		return true
	}
	if response.StatusCode != http.StatusPartialContent {
		return false
	}
	value := strings.TrimSpace(response.Header.Get("Content-Range"))
	if !strings.HasPrefix(strings.ToLower(value), "bytes ") {
		return false
	}
	rangeAndTotal := strings.TrimSpace(value[len("bytes "):])
	byteRange, totalValue, found := strings.Cut(rangeAndTotal, "/")
	if !found {
		return false
	}
	startValue, endValue, found := strings.Cut(strings.TrimSpace(byteRange), "-")
	if !found {
		return false
	}
	start, startErr := strconv.ParseInt(strings.TrimSpace(startValue), 10, 64)
	end, endErr := strconv.ParseInt(strings.TrimSpace(endValue), 10, 64)
	total, totalErr := strconv.ParseInt(strings.TrimSpace(totalValue), 10, 64)
	return startErr == nil && endErr == nil && totalErr == nil &&
		start == 0 && total > 0 && end >= 0 && end+1 == total
}

func nativeDecodeCover(data []byte) []byte {
	if nativeIsCoverImage(data) {
		return data
	}
	// Some poster CDNs return a gzip-wrapped image body while still labelling
	// it as image/jpeg or image/png. Go only transparently decompresses HTTP
	// responses when Content-Encoding is set; unwrap a raw gzip body as a
	// bounded fallback, then validate the actual image signature.
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err == nil {
			decoded, readErr := io.ReadAll(io.LimitReader(reader, nativeCoverMaxBytes+1))
			closeErr := reader.Close()
			if readErr == nil && closeErr == nil &&
				len(decoded) > 0 && len(decoded) <= nativeCoverMaxBytes &&
				nativeIsCoverImage(decoded) {
				return decoded
			}
		}
	}
	encrypted := data
	if bytes.HasPrefix(encrypted, []byte("Salted__")) && len(encrypted) > 16 {
		encrypted = encrypted[16:]
	}
	if len(encrypted) > 0 {
		block, err := aes.NewCipher([]byte("f5d965df75336270"))
		if err == nil {
			plain := make([]byte, (len(encrypted)+aes.BlockSize-1)/aes.BlockSize*aes.BlockSize)
			copy(plain, encrypted)
			cipher.NewCBCDecrypter(block, []byte("97b60394abc2fbe1")).CryptBlocks(plain, plain)
			plain = plain[:len(encrypted)]
			if unpadded, err := pkcs7Unpad(plain, aes.BlockSize); err == nil {
				plain = unpadded
			}
			if nativeIsCoverImage(plain) {
				return plain
			}
		}
	}
	plain := append([]byte(nil), data...)
	key := []byte("2019ysapp7527")
	for index := 0; index < min(100, len(plain)); index++ {
		plain[index] ^= key[index%len(key)]
	}
	if nativeIsCoverImage(plain) {
		return plain
	}
	return data
}

func nativeIsCoverImage(data []byte) bool {
	if isHEICImage(data) {
		return true
	}
	if bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) ||
		bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}) || bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")) {
		return true
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return true
	}
	if len(data) >= 2 && data[0] == 'B' && data[1] == 'M' {
		return true
	}
	if len(data) >= 16 && string(data[4:8]) == "ftyp" {
		switch string(data[8:12]) {
		case "avif", "avis", "heic", "heix", "hevc", "hevx", "mif1":
			return true
		}
	}
	return false
}

func (cache *nativeCoverCache) prune(keep string) {
	if cache.size <= cache.limit && len(cache.entries) <= nativeCoverCacheEntries {
		return
	}
	keys := make([]string, 0, len(cache.entries))
	for key := range cache.entries {
		if key != keep {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return cache.entries[keys[i]].usedAt.Before(cache.entries[keys[j]].usedAt) })
	for _, key := range keys {
		if cache.size <= cache.limit && len(cache.entries) <= nativeCoverCacheEntries {
			break
		}
		if err := os.Remove(filepath.Join(cache.directory, key+".img")); err == nil || errors.Is(err, os.ErrNotExist) {
			cache.size -= cache.entries[key].size
			delete(cache.entries, key)
		}
	}
}
