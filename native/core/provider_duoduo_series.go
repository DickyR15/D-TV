package core

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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

func duoduoDramaFromNative(drama nativeDrama) Drama {
	return Drama{
		ID: drama.ID, Source: drama.Source, SourceID: drama.SourceID,
		Title: drama.Title, Name: drama.Title, Desc: drama.Description,
		Cover: drama.Cover, Episodes: drama.Episodes, Category: drama.Category,
		Heat: drama.Heat, Views: drama.Views, OnlineDate: drama.OnlineDate,
		Tags: append([]string(nil), drama.Tags...), ReleaseStatus: drama.ReleaseStatus,
		SeriesGroupID: drama.SeriesGroupID,
		SeriesSeasonNumber: drama.SeriesSeasonNumber,
		SeriesSeasonUnit: drama.SeriesSeasonUnit,
	}
}

// fetchDuoduoSeriesSeasons uses the provider's title search only to discover
// candidates. A season is accepted only when its normalized base title and an
// explicit season/part number match exactly. Ambiguous duplicate season numbers
// are rejected instead of silently joining unrelated records.
func (downloader *Downloader) fetchDuoduoSeriesSeasons(
	ctx context.Context,
	current nativeDrama,
) ([]nativeDramaSeason, error) {
	base, currentNumber, unit, ok := duoduoSeriesTitle(current.Title)
	if !ok {
		return nil, nil
	}
	baseKey := duoduoSeriesTitleKey(base)
	groupID := duoduoSeriesGroupID(base, unit)
	searchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	byNumber := make(map[int]Drama)
	var unnumbered *Drama
	for page := 1; page <= 3; page++ {
		items, more, err := downloader.fetchDuoduoCatalogPage(
			searchCtx, page, "", base, "hits",
		)
		if err != nil {
			if len(byNumber) == 0 {
				return nil, err
			}
			break
		}
		for _, item := range items {
			if item.ID == "" || item.Source != sourceDuoduo {
				continue
			}
			itemBase, number, itemUnit, hasSeason := duoduoSeriesTitle(item.DisplayTitle())
			if !hasSeason {
				if duoduoSeriesTitleKey(item.DisplayTitle()) == baseKey {
					copy := item
					unnumbered = &copy
				}
				continue
			}
			if duoduoSeriesTitleKey(itemBase) != baseKey || itemUnit != unit {
				continue
			}
			if existing, found := byNumber[number]; found && existing.ID != item.ID {
				return nil, nil
			}
			item.SeriesGroupID = groupID
			item.SeriesSeasonNumber = number
			item.SeriesSeasonUnit = unit
			byNumber[number] = item
		}
		if !more || len(items) == 0 {
			break
		}
	}

	// Some catalogs name the first season with the unsuffixed series title.
	// Only interpret it as season 1 after a numbered sibling was confirmed.
	if _, hasFirst := byNumber[1]; !hasFirst && unnumbered != nil && len(byNumber) > 0 {
		unnumbered.SeriesGroupID = groupID
		unnumbered.SeriesSeasonNumber = 1
		unnumbered.SeriesSeasonUnit = unit
		byNumber[1] = *unnumbered
	}

	// Retain the selected season if the upstream search endpoint omitted it
	// from its first pages; its ID and metadata came from the exact detail row.
	if currentNumber > 0 {
		if _, found := byNumber[currentNumber]; !found {
			item := duoduoDramaFromNative(current)
			item.SeriesGroupID = groupID
			item.SeriesSeasonNumber = currentNumber
			item.SeriesSeasonUnit = unit
			byNumber[currentNumber] = item
		}
	}
	if len(byNumber) < 2 {
		return nil, nil
	}

	numbers := make([]int, 0, len(byNumber))
	for number := range byNumber {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	seasons := make([]nativeDramaSeason, 0, len(numbers))
	for _, number := range numbers {
		item := byNumber[number]
		item.SeriesGroupID = groupID
		item.SeriesSeasonNumber = number
		item.SeriesSeasonUnit = unit
		native := nativeNormalize(item)
		native.SeriesGroupID = groupID
		native.SeriesSeasonNumber = number
		native.SeriesSeasonUnit = unit
		label := fmt.Sprintf("第%d%s", number, unit)
		seasons = append(seasons, nativeDramaSeason{
			Drama: native,
			Number: number,
			Label: label,
			Subtitle: item.Remark,
			Upcoming: item.Episodes <= 0,
			Playable: item.Episodes > 0,
		})
	}
	return seasons, nil
}
