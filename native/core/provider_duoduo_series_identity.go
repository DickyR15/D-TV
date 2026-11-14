package core

import (
	"regexp"
	"strings"
)

var (
	duoduoIMDbIDPattern = regexp.MustCompile("(?i)\\btt[0-9]{7,12}\\b")
	duoduoDoubanIDPattern = regexp.MustCompile("[0-9]{4,}")
)

// Explicit, verified alternate titles; no fuzzy title matching is used.
var duoduoVerifiedSeriesAliases = map[string][]string{
	"chicago-med": {
		"芝加哥急救", "芝加哥醫院", "芝加哥医院", "芝加哥医魂",
		"芝加哥醫魂", "芝加哥醫情", "芝加哥医情", "Chicago Med",
	},
}

func duoduoNormalizeIMDbID(value string) string {
	return strings.ToLower(duoduoIMDbIDPattern.FindString(strings.TrimSpace(value)))
}

func duoduoNormalizeDoubanID(value string) string {
	return duoduoDoubanIDPattern.FindString(strings.TrimSpace(value))
}

func duoduoIMDbIDFromRow(row map[string]any) string {
	for _, key := range []string{"vod_imdb_id", "imdb_id", "imdbId", "imdb", "vod_imdb"} {
		if id := duoduoNormalizeIMDbID(duoduoString(row[key])); id != "" { return id }
	}
	for _, key := range []string{"vod_content", "vod_blurb", "vod_name", "vod_en", "vod_original_name", "original_title"} {
		value := providerHTMLPlainText(duoduoString(row[key]))
		if id := duoduoNormalizeIMDbID(value); id != "" { return id }
	}
	return ""
}

func duoduoVerifiedSeriesAliasKey(base string) string {
	key := duoduoSeriesTitleKey(base)
	for canonical, aliases := range duoduoVerifiedSeriesAliases {
		for _, alias := range aliases { if key == duoduoSeriesTitleKey(alias) { return canonical } }
	}
	return ""
}

func duoduoSeriesSearchTitles(base string) []string {
	aliasKey := duoduoVerifiedSeriesAliasKey(base)
	if aliasKey == "" { return []string{strings.TrimSpace(base)} }
	seen := make(map[string]bool)
	out := make([]string, 0, len(duoduoVerifiedSeriesAliases[aliasKey])+1)
	add := func(value string) {
		value = strings.TrimSpace(value)
		key := duoduoSeriesTitleKey(value)
		if key != "" && !seen[key] { seen[key] = true; out = append(out, value) }
	}
	add(base)
	for _, alias := range duoduoVerifiedSeriesAliases[aliasKey] { add(alias) }
	return out
}

func duoduoCanonicalSeriesGroupID(base, unit, imdbID, doubanID string) string {
	identity := ""
	if alias := duoduoVerifiedSeriesAliasKey(base); alias != "" {
		identity = "alias:" + alias
	} else if id := duoduoNormalizeIMDbID(imdbID); id != "" {
		identity = "imdb:" + id
	} else if id := duoduoNormalizeDoubanID(doubanID); id != "" {
		identity = "douban:" + id
	} else {
		return duoduoSeriesGroupID(base, unit)
	}
	return "duoduo-series:" + identity + ":" + unit
}

func duoduoSameSeriesIdentity(currentBase, candidateBase string, currentIMDbID, currentDoubanID, candidateIMDbID, candidateDoubanID string) bool {
	currentIMDb, candidateIMDb := duoduoNormalizeIMDbID(currentIMDbID), duoduoNormalizeIMDbID(candidateIMDbID)
	if currentIMDb != "" && candidateIMDb != "" && currentIMDb != candidateIMDb {
		return false
	}
	currentDouban, candidateDouban := duoduoNormalizeDoubanID(currentDoubanID), duoduoNormalizeDoubanID(candidateDoubanID)
	if currentDouban != "" && candidateDouban != "" && currentDouban != candidateDouban {
		return false
	}
	currentAlias, candidateAlias := duoduoVerifiedSeriesAliasKey(currentBase), duoduoVerifiedSeriesAliasKey(candidateBase)
	if currentAlias != "" || candidateAlias != "" {
		return currentAlias != "" && currentAlias == candidateAlias
	}
	// Preserve exact-base matching where the provider assigns per-season IDs.
	if duoduoSeriesTitleKey(currentBase) == duoduoSeriesTitleKey(candidateBase) {
		return true
	}
	if currentIMDb != "" && candidateIMDb != "" {
		return currentIMDb == candidateIMDb
	}
	if currentDouban != "" && candidateDouban != "" {
		return currentDouban == candidateDouban
	}
	return false
}

func duoduoSeriesCandidateScore(drama Drama) int {
	score := 0
	if duoduoDramaHasOfficialCover(drama) { score += 8 }
	if duoduoDramaEpisodeCount(drama) > 0 { score += 4 }
	if duoduoNormalizeIMDbID(drama.IMDbID) != "" { score += 2 }
	if duoduoNormalizeDoubanID(drama.DoubanID) != "" { score++ }
	if strings.TrimSpace(drama.Remark) != "" { score++ }
	if strings.TrimSpace(drama.Desc) != "" { score++ }
	return score
}

func duoduoMergeDuplicateSeasonCandidates(first, second Drama) Drama {
	chosen, other := first, second
	if duoduoSeriesCandidateScore(second) > duoduoSeriesCandidateScore(first) ||
		duoduoSeriesCandidateScore(second) == duoduoSeriesCandidateScore(first) && second.ID != "" && (first.ID == "" || second.ID < first.ID) {
		chosen, other = second, first
	}
	addresses := append([]string{}, nativeCoverAddressesFromDrama(chosen)...)
	addresses = append(addresses, nativeCoverAddressesFromDrama(other)...)
	addresses = nativeUniqueCoverAddresses(addresses...)
	if len(addresses) > 0 { chosen.Cover = addresses[0]; chosen.CoverURL = addresses[0]; chosen.CoverCandidates = append([]string{}, addresses[1:]...) }
	if duoduoDramaEpisodeCount(chosen) == 0 { if count := duoduoDramaEpisodeCount(other); count > 0 { chosen.Episodes = count; chosen.EpisodeCount = count; chosen.TotalEpisode = count } }
	if chosen.IMDbID == "" { chosen.IMDbID = other.IMDbID }
	if chosen.DoubanID == "" { chosen.DoubanID = other.DoubanID }
	if strings.TrimSpace(chosen.Remark) == "" { chosen.Remark = other.Remark }
	if strings.TrimSpace(chosen.Desc) == "" { chosen.Desc = other.Desc }
	return chosen
}
