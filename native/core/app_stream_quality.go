package core

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var (
	nativeHLSResolutionPattern = regexp.MustCompile(`(?i)\bRESOLUTION\s*=\s*"?[0-9]+\s*[xX]\s*([0-9]+)`)
	nativeHLSHeightPattern     = regexp.MustCompile(`(?i)\bHEIGHT\s*=\s*"?([0-9]+)`)
	nativeHLSNamedQuality      = regexp.MustCompile(`(?i)(?:^|[^0-9])([0-9]{3,4})p(?:[^0-9]|$)`)
)

// nativeHLSVariantQuality extracts a vertical resolution only from explicit
// HLS resolution/height metadata or a conventional 360p/720p URI/name.
// Unknown variants are deliberately retained rather than guessed.
func nativeHLSVariantQuality(attributes, uri string) int {
	if match := nativeHLSResolutionPattern.FindStringSubmatch(attributes); len(match) == 2 {
		if value, err := strconv.Atoi(match[1]); err == nil && value > 0 {
			return value
		}
	}
	if match := nativeHLSHeightPattern.FindStringSubmatch(attributes); len(match) == 2 {
		if value, err := strconv.Atoi(match[1]); err == nil && value > 0 {
			return value
		}
	}
	for _, value := range []string{attributes, uri} {
		if match := nativeHLSNamedQuality.FindStringSubmatch(value); len(match) == 2 {
			if height, err := strconv.Atoi(match[1]); err == nil && height > 0 {
				return height
			}
		}
	}
	return 0
}

// nativeFilterHLSMasterVariants removes only variants with a known height below
// 720p. 720p, higher resolutions, and unlabelled variants remain available.
// Media playlists and audio/subtitle renditions are left untouched.
func nativeFilterHLSMasterVariants(body string) (string, int, error) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	output := make([]string, 0, len(lines))
	removed := 0
	sawStreamVariant := false
	keptStreamVariants := 0

	for index := 0; index < len(lines); index++ {
		line := lines[index]
		text := strings.TrimSpace(line)

		if strings.HasPrefix(text, "#EXT-X-STREAM-INF:") {
			sawStreamVariant = true
			uriIndex := index + 1
			for uriIndex < len(lines) && strings.TrimSpace(lines[uriIndex]) == "" {
				uriIndex++
			}
			uri := ""
			if uriIndex < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[uriIndex]), "#") {
				uri = strings.TrimSpace(lines[uriIndex])
			}
			quality := nativeHLSVariantQuality(text, uri)
			if quality > 0 && quality < nativeMinimumPlaybackQuality {
				removed++
				if uri != "" {
					index = uriIndex
				}
				continue
			}
			keptStreamVariants++
			output = append(output, line)
			continue
		}

		if strings.HasPrefix(text, "#EXT-X-I-FRAME-STREAM-INF:") ||
			(strings.HasPrefix(text, "#EXT-X-MEDIA:") && strings.Contains(strings.ToUpper(text), "TYPE=VIDEO")) {
			quality := nativeHLSVariantQuality(text, "")
			if quality > 0 && quality < nativeMinimumPlaybackQuality {
				removed++
				continue
			}
		}

		output = append(output, line)
	}

	if sawStreamVariant && removed > 0 && keptStreamVariants == 0 {
		return "", removed, errors.New("播放清單沒有 720P 以上的播放線路")
	}
	return strings.Join(output, "\n"), removed, nil
}
