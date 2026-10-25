package core

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

var hongguoAppGenres = []struct {
	key   string
	scene string
	name  string
}{
	{key: "short_play", scene: "default", name: "真人剧"},
	{key: "comic_series", scene: "comic_series", name: "漫剧"},
	{key: "ai_series", scene: "ai_series", name: "AI剧"},
}

var hongguoWebCatalogRoutes = []struct {
	path string
	name string
}{
	{path: "real-drama", name: "真人剧"},
	{path: "comic-drama", name: "漫剧"},
	{path: "ai-drama", name: "AI剧"},
	{path: "comic", name: "动漫"},
}

type libraryMoreKey struct{}

type libraryUpdateKey struct{}

// libraryWebsiteBackfillKey permits the slower public-website history scan only
// from the bounded Red Fruit update task, never from health checks or normal UI
// catalog requests.
type libraryWebsiteBackfillKey struct{}

type libraryKnownKey struct{}

func shouldFetchHongguoWebsiteHistory(ctx context.Context, category string, state *hongguoCatalogState) bool {
	enabled, _ := ctx.Value(libraryWebsiteBackfillKey{}).(bool)
	return enabled && category == "" && !hongguoAppCatalogHasMore(state)
}

func (downloader *Downloader) fetchHongguoAppCatalog(ctx context.Context) ([]Drama, error) {
	return downloader.fetchHongguoAppCatalogCategory(ctx, "")
}

 
// fetchHongguoWebsiteCatalogPage extends the App API catalog with one historical
// page from each public website category. The per-route cursors are persisted
// with the App feed cursors, so long catalog updates can resume after restarts.
func (downloader *Downloader) fetchHongguoWebsiteCatalogPage(ctx context.Context) ([]Drama, error) {
	// Called from fetchHongguoAppCatalogCategory while catalogMu is already held.
	// Do not acquire catalogMu here again.
	client := downloader.hongguoClient()

	type pendingRoute struct {
		path string
		name string
		key  string
		page int
	}
	var pending []pendingRoute
	state := downloader.hongguoCatalogSnapshot()
	for _, route := range hongguoWebCatalogRoutes {
		key := "web:" + route.path
		cursor := state.Feeds[key]
		if cursor.Exhausted {
			continue
		}
		page := 1
		if cursor.Initialized {
			page = cursor.Offset + 1
		}
		if page > 500 {
			continue
		}
		pending = append(pending, pendingRoute{path: route.path, name: route.name, key: key, page: page})
	}
	if len(pending) == 0 {
		return nil, nil
	}

	type pageResult struct {
		route pendingRoute
		items []Drama
		total int
		err   error
	}
	results := make(chan pageResult, len(pending))
	for _, entry := range pending {
		entry := entry
		go func() {
			items, total, err := downloader.fetchHongguoCategoryPage(
				ctx,
				entry.path+"?page="+strconv.Itoa(entry.page),
				entry.name,
			)
			results <- pageResult{route: entry, items: items, total: total, err: err}
		}()
	}

	seen := make(map[string]bool)
	var dramas []Drama
	var failures []error
	successes := 0
	for range pending {
		result := <-results
		if result.err != nil {
			failures = append(failures, fmt.Errorf("%s 第 %d 頁: %w", result.route.name, result.route.page, result.err))
			continue
		}
		successes++
		cursor := state.Feeds[result.route.key]
		previousSignature := cursor.PageSignature
		ids := make([]string, 0, len(result.items))
		for _, drama := range result.items {
			if drama.ID != "" {
				ids = append(ids, drama.ID)
			}
		}
		sort.Strings(ids)
		signature := ""
		if len(ids) > 0 {
			signature = fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(ids, "\n"))))
		}
		repeatedPage := signature != "" && signature == previousSignature

		cursor.Offset = result.route.page
		cursor.Initialized = true
		cursor.UpdatedAt = time.Now()
		cursor.PageSignature = signature
		hasMore := len(result.items) >= 24
		if result.total > result.route.page {
			hasMore = true
		}
		if result.route.page >= 500 || len(result.items) == 0 || repeatedPage {
			hasMore = false
		}
		cursor.Exhausted = !hasMore

		client.mu.Lock()
		client.state.Feeds[result.route.key] = cursor
		client.mu.Unlock()

		for _, drama := range result.items {
			if drama.ID == "" || seen[drama.ID] {
				continue
			}
			seen[drama.ID] = true
			dramas = append(dramas, drama)
		}
	}
	sort.SliceStable(dramas, func(i, j int) bool {
		return dramas[i].DisplayTitle() < dramas[j].DisplayTitle()
	})
	if len(failures) > 0 {
		return dramas, errors.Join(failures...)
	}
	if successes == 0 {
		return nil, errors.New("红果网站历史目录没有成功取得任何分页")
	}
	return dramas, nil
}

func (downloader *Downloader) fetchHongguoAppCatalogCategory(ctx context.Context, category string) ([]Drama, error) {
	genres := hongguoAppGenres
	if category != "" {
		genres = nil
		for _, genre := range hongguoAppGenres {
			if genre.key == category {
				genres = append(genres, genre)
			}
		}
		if len(genres) == 0 {
			return nil, errors.New("红果分类无效")
		}
	}
	feedKey := func(key string) string {
		if category != "" {
			return "category:" + key
		}
		return key
	}
	client := downloader.hongguoClient()
	client.catalogMu.Lock()
	defer client.catalogMu.Unlock()
	more, _ := ctx.Value(libraryMoreKey{}).(bool)
	update, _ := ctx.Value(libraryUpdateKey{}).(bool)
	known, _ := ctx.Value(libraryKnownKey{}).(map[string]bool)
	state := downloader.hongguoCatalogSnapshot()
	pageLimit := downloader.cfg.MaxPagesPerSort
	if pageLimit < 1 {
		pageLimit = defaultConfig().MaxPagesPerSort
	}
	if pageLimit > 200 {
		pageLimit = 200
	}
	headLimit := min(pageLimit, 3)
	if background, _ := ctx.Value(backgroundCatalogKey{}).(bool); !background {
		pageLimit = min(pageLimit, max(1, 3/len(genres)))
	}
	roundLimit := max(pageLimit, headLimit)
	if update {
		roundLimit += headLimit
	}
	type feedScan struct {
		cursor hongguoCatalogCursor
		tail   hongguoCatalogCursor
		head   bool
		done   bool
		pages  int
	}
	scans := make([]feedScan, len(genres))
	for index, genre := range genres {
		cursor := state.Feeds[feedKey(genre.key)]
		if !more && cursor.Initialized {
			scans[index].head = true
			scans[index].tail = cursor
		} else {
			scans[index].cursor = cursor
			scans[index].done = cursor.Exhausted
		}
	}
	seen := map[string]int{}
	var dramas []Drama
	var failures []error
	for round := 0; round < roundLimit; round++ {
		active := false
		for index, genre := range genres {
			scan := &scans[index]
			if scan.done || !scan.head && scan.pages >= pageLimit {
				continue
			}
			if err := ctx.Err(); err != nil {
				return dramas, err
			}
			active = true
			cursor := scan.cursor
			if age := time.Since(cursor.UpdatedAt); age < 0 || age > 30*time.Minute {
				cursor.SessionID = ""
			}
			payload := map[string]any{
				"req_scene": genre.scene, "offset": cursor.Offset, "limit": 54,
				"req_type": "only_content", "need_selector_panel": false, "client_req_type": 3,
				"session_id": cursor.SessionID, "filter_ids": "",
				"select_items": map[string]any{
					"genre": []string{genre.key}, "sort": []string{"online_time"}, "gender": []string{},
					"category_dim_theme": []string{}, "category_dim_role": []string{}, "category_dim_epoch": []string{},
					"online_time": []string{}, "creation_status": []string{},
				},
			}
			if cursor.Offset > 0 {
				payload["client_req_type"] = 2
			}
			var items []Drama
			var nextCursor hongguoCatalogCursor
			var pageErr error
			newItems := 0
			for attempt := 0; attempt < 2; attempt++ {
				result, err := downloader.hongguoAppRequest(ctx, http.MethodPost, "/reading/distribution/category/landpage/v/", nil, payload)
				retryable := err == nil || cursor.SessionID != ""
				items = nil
				if err == nil {
					items, nextCursor, err = parseHongguoCatalogPage(result, cursor, genre.name)
				}
				newItems = 0
				for _, drama := range items {
					if !known[drama.ID] {
						newItems++
					}
					if position, exists := seen[drama.ID]; exists {
						dramas[position] = mergeDramaMetadata(drama, dramas[position])
					} else {
						seen[drama.ID] = len(dramas)
						dramas = append(dramas, drama)
					}
				}
				pageErr = err
				if err == nil {
					break
				}
				reportLibraryProgress(ctx, sourceHongguo, items, nil, false)
				var backoff *requestBackoff
				if attempt > 0 || !retryable || errors.As(err, &backoff) || ctx.Err() != nil {
					break
				}
				payload["session_id"] = ""
			}
			if err := ctx.Err(); err != nil {
				return dramas, err
			}
			if pageErr != nil {
				failures = append(failures, fmt.Errorf("%s: %w", genre.name, pageErr))
				scan.done = true
				continue
			}
			cursor = nextCursor
			scan.pages++
			scan.cursor = cursor
			scan.done = cursor.Exhausted
			checkpoint := cursor
			if scan.head && newItems == 0 && !cursor.Exhausted {
				scan.done = true
				if scan.tail.Exhausted || scan.tail.Offset >= cursor.Offset {
					checkpoint = scan.tail
				}
			}
			if scan.head && (scan.done || scan.pages >= headLimit) {
				scan.done = true
				if update && !checkpoint.Exhausted {

					scan.cursor = checkpoint
					scan.head, scan.done, scan.pages = false, false, 0
				}
			}
			client.mu.Lock()
			client.state.Feeds[feedKey(genre.key)] = checkpoint
			client.mu.Unlock()
			reportLibraryProgress(ctx, sourceHongguo, items, nil, false)
		}
		if !active {
			break
		}
	}
	sort.SliceStable(dramas, func(left, right int) bool { return dramas[left].DisplayTitle() < dramas[right].DisplayTitle() })
	return dramas, errors.Join(failures...)
}

func parseHongguoCatalogPage(result map[string]any, cursor hongguoCatalogCursor, category string) ([]Drama, hongguoCatalogCursor, error) {
	data := nestedMap(result, "data")
	rows, valid := data["video_data"].([]any)
	if !valid {
		return nil, cursor, errors.New("App 分类数据格式异常")
	}
	items := make([]Drama, 0, len(rows))
	ids := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		drama := hongguoDramaFromAny(row, category)
		if drama.ID != "" {
			items = append(items, drama)
			if !seen[drama.ID] {
				seen[drama.ID] = true
				ids = append(ids, drama.ID)
			}
		}
	}
	if len(rows) > 0 && len(items) == 0 {
		return items, cursor, errors.New("App 分类未返回可识别的剧集")
	}
	next, parseErr := strconv.Atoi(mapString(data, "next_offset"))
	hasMore, paginationOK := data["has_more"].(bool)
	if !paginationOK || hasMore && parseErr != nil {
		return items, cursor, errors.New("App 分页标记无效，已保留上次位置")
	}
	if parseErr != nil {
		next = cursor.Offset + len(rows)
	}
	lastID, signature := "", ""
	if len(items) > 0 {
		lastID = items[len(items)-1].ID
		sort.Strings(ids)
		signature = fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(ids, "\n"))))
	}
	if hasMore && (len(items) == 0 || next <= cursor.Offset || next > 1_000_000 || signature == cursor.PageSignature) {
		return items, cursor, errors.New("App 分页未前进，已保留上次位置")
	}
	if !hasMore && (next < cursor.Offset || next > 1_000_000) {
		next = cursor.Offset
	}
	session := mapString(data, "session_id")
	if len(session) > 4096 || strings.ContainsAny(session, "\r\n\x00") {
		return items, cursor, errors.New("App 分页会话无效，已保留上次位置")
	}
	cursor.Exhausted = !hasMore
	cursor.Initialized = true
	cursor.Offset = next
	cursor.SessionID = session
	cursor.LastID = lastID
	cursor.PageSignature = signature
	cursor.UpdatedAt = time.Now()
	return items, cursor, nil
}
