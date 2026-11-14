package core

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	duoduoSeriesCatalogTimeout = 10 * time.Second
	duoduoSeriesHydrationTimeout = 60 * time.Second
	duoduoSeasonHydrationTimeout = 12 * time.Second
)

var (
	duoduoChineseSeasonTitle = regexp.MustCompile(`(?i)^(.+?)\s*第\s*([0-9]{1,3}|[零〇一二三四五六七八九十百]+)\s*(季|部)\s*$`)
	duoduoEnglishSeasonTitle = regexp.MustCompile(`(?i)^(.+?)\s+(?:season\s*([0-9]{1,3})|s([0-9]{1,3}))\s*$`)
)

func duoduoChineseSeasonNumber(raw string) int {
	if value, err := strconv.Atoi(raw); err == nil {
		if value > 0 && value <= 999 {
			return value
		}
		return 0
	}
	digits := map[rune]int{
		'零': 0, '〇': 0, '一': 1, '二': 2, '三': 3, '四': 4,
		'五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
	}
	total, current := 0, 0
	for _, value := range raw {
		switch value {
		case '十':
			if current == 0 {
				current = 1
			}
			total += current * 10
			current = 0
		case '百':
			if current == 0 {
				return 0
			}
			total += current * 100
			current = 0
		default:
			digit, ok := digits[value]
			if !ok {
				return 0
			}
			current = digit
		}
	}
	number := total + current
	if number < 1 || number > 999 {
		return 0
	}
	return number
}

// duoduoSeriesTitle extracts an explicit season/part marker. It deliberately
// does not infer a season from a shared or similar title alone.
func duoduoSeriesTitle(title string) (base string, number int, unit string, ok bool) {
	title = strings.TrimSpace(strings.Join(strings.Fields(title), " "))
	if match := duoduoChineseSeasonTitle.FindStringSubmatch(title); len(match) == 4 {
		base = strings.TrimSpace(match[1])
		number = duoduoChineseSeasonNumber(match[2])
		unit = match[3]
		return base, number, unit, base != "" && number > 0
	}
	if match := duoduoEnglishSeasonTitle.FindStringSubmatch(title); len(match) == 4 {
		base = strings.TrimSpace(match[1])
		raw := firstNonEmpty(match[2], match[3])
		number, _ = strconv.Atoi(raw)
		unit = "季"
		return base, number, unit, base != "" && number > 0 && number <= 999
	}
	return "", 0, "", false
}

func duoduoSeriesTitleKey(value string) string {
	value = strings.NewReplacer(
		"（", "(", "）", ")", "［", "[", "］", "]", "　", " ",
	).Replace(strings.TrimSpace(value))
	return strings.ToLower(strings.Join(strings.Fields(value), ""))
}

func duoduoSeriesGroupID(base, unit string) string {
	return "duoduo-series:" + duoduoSeriesTitleKey(base) + ":" + unit
}

var duoduoEpisodeCountPattern = regexp.MustCompile(`(?i)(?:更新至|全|共)?\s*([0-9]{1,6})\s*(?:集|话|期|回|章)`)

func duoduoEpisodeValueCount(value any) int {
	raw := strings.TrimSpace(fmt.Sprint(value))
	if raw == "" || raw == "<nil>" {
		return 0
	}
	if count, err := strconv.Atoi(raw); err == nil && count > 0 && count <= 100000 {
		return count
	}
	if number, err := strconv.ParseFloat(raw, 64); err == nil &&
		number > 0 && number <= 100000 && number == float64(int(number)) {
		return int(number)
	}
	if match := duoduoEpisodeCountPattern.FindStringSubmatch(raw); len(match) == 2 {
		if count, err := strconv.Atoi(match[1]); err == nil && count > 0 && count <= 100000 {
			return count
		}
	}
	return 0
}

// Duoduo catalog rows store episode totals in several legacy aliases; Episodes
// itself may be empty. Resolve the known fields before classifying a season.
func duoduoDramaEpisodeCount(drama Drama) int {
	for _, value := range []any{
		drama.Episodes,
		drama.EpisodeCount,
		drama.EpisodeCountSnake,
		drama.TotalEpisode,
		drama.TotalEpisodeSnake,
		drama.ChapterCount,
		drama.ChapterCountSnake,
		drama.Total,
		drama.Remark,
	} {
		if count := duoduoEpisodeValueCount(value); count > 0 {
			return count
		}
	}
	return 0
}

func duoduoDramaFromNative(drama nativeDrama) Drama {
	return Drama{
		ID: drama.ID, Source: drama.Source, SourceID: drama.SourceID,
		Title: drama.Title, Name: drama.Title, Desc: drama.Description,
		Cover: drama.Cover, CoverCandidates: append([]string(nil), drama.CoverCandidates...),
		DoubanID: drama.DoubanID, IMDbID: drama.IMDbID,
		Episodes: drama.Episodes, Category: drama.Category,
		Heat: drama.Heat, Views: drama.Views, OnlineDate: drama.OnlineDate,
		Tags: append([]string(nil), drama.Tags...), ReleaseStatus: drama.ReleaseStatus,
		SeriesGroupID: drama.SeriesGroupID,
		SeriesSeasonNumber: drama.SeriesSeasonNumber,
		SeriesSeasonUnit: drama.SeriesSeasonUnit,
	}
}

func duoduoDramaHasOfficialCover(drama Drama) bool {
	return len(providerCoverAddresses(drama.Cover, duoduoPublicEntryURL)) > 0 ||
		len(providerCoverAddresses(drama.CoverURL, duoduoPublicEntryURL)) > 0 ||
		len(drama.CoverCandidates) > 0
}

func duoduoMergeSameSeasonMetadata(catalog, detail Drama) Drama {
	if catalog.ID == "" || catalog.ID != detail.ID ||
		catalog.Source != sourceDuoduo || detail.Source != sourceDuoduo ||
		catalog.SourceID == "" || catalog.SourceID != detail.SourceID {
		return catalog
	}
	detailAddresses := nativeCoverAddressesFromDrama(detail)
	if len(detailAddresses) > 0 {
		// Keep the previous primary and alternate URLs as lower-priority
		// candidates when the official detail endpoint refreshes the main poster.
		previousAddresses := nativeCoverAddressesFromDrama(catalog)
		catalog.Cover = detailAddresses[0]
		catalog.CoverURL = detailAddresses[0]
		candidates := append([]string{}, detailAddresses[1:]...)
		candidates = append(candidates, previousAddresses...)
		catalog.CoverCandidates = nativeUniqueCoverAddresses(candidates...)
	}
	if count := duoduoDramaEpisodeCount(detail); count > 0 {
		catalog.Episodes = count
		catalog.EpisodeCount = count
		catalog.TotalEpisode = count
	}
	if strings.TrimSpace(catalog.Remark) == "" {
		catalog.Remark = detail.Remark
	}
	return catalog
}

func (downloader *Downloader) hydrateDuoduoSeriesSeason(ctx context.Context, catalog Drama) Drama {
	if catalog.Source != sourceDuoduo || catalog.SourceID == "" {
		return catalog
	}

	// Resolve the season through the same detail + aggregate parser used by
	// search-result playback. Catalog rows can omit the episode count even
	// though search playback can resolve direct URLs from search_aggregate.
	detail, chapters, _ := downloader.fetchDuoduoDetailWithCategory(
		ctx, catalog.SourceID, catalog.CategoryName,
	)
	// Detail identity and poster/count metadata remain useful even when the
	// aggregate currently has no public direct line. Keep the season card
	// enriched; the normal detail resolver will re-check playback when opened.
	if detail.ID != catalog.ID || detail.SourceID != catalog.SourceID {
		return catalog
	}
	detailEpisodeCount := duoduoDramaEpisodeCount(detail)
	catalog = duoduoMergeSameSeasonMetadata(catalog, detail)
	// When the official detail row omits a count, the shared detail+aggregate
	// resolver's actual chapters are more authoritative than a stale catalog
	// count. Do not keep an inflated search-card total in that case.
	if detailEpisodeCount == 0 && len(chapters) > 0 {
		maxEpisode := 0
		for index, chapter := range chapters {
			number := duoduoEpisodeNumber(chapter.Title, index+1)
			if number > maxEpisode {
				maxEpisode = number
			}
		}
		if maxEpisode > 0 {
			catalog.Episodes = maxEpisode
			catalog.EpisodeCount = maxEpisode
			catalog.TotalEpisode = maxEpisode
		}
	}
	return catalog
}

func duoduoSeasonMarkedUpcoming(remark string) bool {
	value := strings.ToLower(strings.TrimSpace(remark))
	for _, marker := range []string{
		"即将上线", "即將上線", "待上线", "待上線", "即将播出", "即將播出",
		"敬请期待", "敬請期待", "尚未开播", "尚未開播", "未开播", "未開播",
	} {
		if strings.Contains(value, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

// fetchDuoduoSeriesSeasons uses the provider's title search only to discover
// candidates. A season is accepted only when its normalized base title and an
// explicit season/part number match exactly. Ambiguous duplicate season numbers
// are rejected instead of silently joining unrelated records.// fetchDuoduoSeriesSeasons uses the provider's title search only to discover
// candidates. A season is accepted only when its normalized base title and an
// explicit season/part number match exactly. Ambiguous duplicate season numbers
// are rejected instead of silently joining unrelated records.
func (downloader *Downloader) fetchDuoduoSeriesSeasons(
	ctx context.Context,
	current nativeDrama,
) ([]nativeDramaSeason, error) {
	base := strings.TrimSpace(current.Title)
	currentNumber, unit := 0, ""
	if parsedBase, number, parsedUnit, found := duoduoSeriesTitle(current.Title); found {
		base, currentNumber, unit = parsedBase, number, parsedUnit
	}
	if base == "" { return nil, nil }
	catalogCtx, cancelCatalog := context.WithTimeout(ctx, duoduoSeriesCatalogTimeout)
	defer cancelCatalog()

	byNumber := make(map[int]Drama)
	var unnumbered *Drama
	if currentNumber == 0 { item := duoduoDramaFromNative(current); unnumbered = &item }
	seenIDs := make(map[string]bool)
	for queryIndex, searchTitle := range duoduoSeriesSearchTitles(base) {
		pageLimit := 3
		if queryIndex > 0 { pageLimit = 1 }
		for page := 1; page <= pageLimit; page++ {
			items, more, err := downloader.fetchDuoduoCatalogPage(catalogCtx, page, "", searchTitle, "hits")
			if err != nil {
				if len(byNumber) == 0 && queryIndex == 0 { return nil, err }
				break
			}
			for _, item := range items {
				if item.ID == "" || item.Source != sourceDuoduo || seenIDs[item.ID] { continue }
				seenIDs[item.ID] = true
				itemBase, number, itemUnit, hasSeason := duoduoSeriesTitle(item.DisplayTitle())
				if !hasSeason {
					if duoduoSameSeriesIdentity(base, item.DisplayTitle(), current.IMDbID, current.DoubanID, item.IMDbID, item.DoubanID) {
						copy := item
						if unnumbered == nil { unnumbered = &copy } else {
							merged := duoduoMergeDuplicateSeasonCandidates(*unnumbered, copy)
							unnumbered = &merged
						}
					}
					continue
				}
				if !duoduoSameSeriesIdentity(base, itemBase, current.IMDbID, current.DoubanID, item.IMDbID, item.DoubanID) { continue }
				if unit == "" { unit = itemUnit } else if itemUnit != unit { continue }
				if existing, found := byNumber[number]; found {
					byNumber[number] = duoduoMergeDuplicateSeasonCandidates(existing, item)
				} else { byNumber[number] = item }
			}
			if !more || len(items) == 0 { break }
		}
	}
	if unit == "" { return nil, nil }
	groupID := duoduoCanonicalSeriesGroupID(base, unit, current.IMDbID, current.DoubanID)
	if _, hasFirst := byNumber[1]; !hasFirst && unnumbered != nil && len(byNumber) > 0 {
		unnumbered.SeriesGroupID = groupID
		unnumbered.SeriesSeasonNumber = 1
		unnumbered.SeriesSeasonUnit = unit
		byNumber[1] = *unnumbered
	}

	type hydratedSeason struct { number int; drama Drama }
	needsHydration := make([]int, 0, len(byNumber))
	for number, item := range byNumber { if item.ID != current.ID { needsHydration = append(needsHydration, number) } }
	sort.Ints(needsHydration)
	hydrationCtx, cancelHydration := context.WithTimeout(ctx, duoduoSeriesHydrationTimeout)
	defer cancelHydration()
	results := make(chan hydratedSeason, len(needsHydration))
	var workers sync.WaitGroup
	semaphore := make(chan struct{}, 3)
	for _, number := range needsHydration {
		item := byNumber[number]
		workers.Add(1)
		go func(number int, item Drama) {
			defer workers.Done()
			select { case semaphore <- struct{}{}: case <-hydrationCtx.Done(): return }
			seasonCtx, cancelSeason := context.WithTimeout(hydrationCtx, duoduoSeasonHydrationTimeout)
			hydrated := downloader.hydrateDuoduoSeriesSeason(seasonCtx, item)
			cancelSeason()
			<-semaphore
			results <- hydratedSeason{number: number, drama: hydrated}
		}(number, item)
	}
	go func() { workers.Wait(); close(results) }()
	for result := range results { byNumber[result.number] = result.drama }

	currentItem := duoduoDramaFromNative(current)
	if currentNumber > 0 {
		currentItem.SeriesGroupID = groupID
		currentItem.SeriesSeasonNumber = currentNumber
		currentItem.SeriesSeasonUnit = unit
		if existing, found := byNumber[currentNumber]; found {
			byNumber[currentNumber] = duoduoMergeDuplicateSeasonCandidates(existing, currentItem)
		} else { byNumber[currentNumber] = currentItem }
	} else if first, found := byNumber[1]; found {
		byNumber[1] = duoduoMergeDuplicateSeasonCandidates(first, currentItem)
	}
	if len(byNumber) < 2 { return nil, nil }

	numbers := make([]int, 0, len(byNumber))
	for number := range byNumber { numbers = append(numbers, number) }
	sort.Ints(numbers)
	seasons := make([]nativeDramaSeason, 0, len(numbers))
	for _, number := range numbers {
		item := byNumber[number]
		item.SeriesGroupID, item.SeriesSeasonNumber, item.SeriesSeasonUnit = groupID, number, unit
		episodeCount := duoduoDramaEpisodeCount(item)
		upcoming := duoduoSeasonMarkedUpcoming(item.Remark)
		native := nativeNormalize(item)
		native.Episodes, native.EpisodesVerified = episodeCount, episodeCount > 0
		native.SeriesGroupID, native.SeriesSeasonNumber, native.SeriesSeasonUnit = groupID, number, unit
		seasons = append(seasons, nativeDramaSeason{
			Drama: native, Number: number, Label: fmt.Sprintf("第%d%s", number, unit),
			Subtitle: item.Remark, Upcoming: upcoming, Playable: !upcoming,
		})
	}
	return seasons, nil
}
