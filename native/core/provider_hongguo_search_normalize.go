package core

import (
	_ "embed"
	"strings"
)

// The OpenCC character dictionaries are used only to canonicalize search keys;
// displayed titles and source/API query strings are left unchanged.
//go:embed opencc/STCharacters.txt
var hongguoSTCharacters string

//go:embed opencc/TWVariants.txt
var hongguoTWVariants string

var hongguoVariantToSimplified = buildHongguoVariantMap()

func buildHongguoVariantMap() map[rune]rune {
	type entry struct {
		key    rune
		values []rune
	}
	parse := func(dictionary string) []entry {
		var entries []entry
		for _, line := range strings.Split(dictionary, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			keyRunes := []rune(fields[0])
			if len(keyRunes) != 1 {
				continue
			}
			item := entry{key: keyRunes[0]}
			for _, value := range fields[1:] {
				valueRunes := []rune(value)
				if len(valueRunes) == 1 {
					item.values = append(item.values, valueRunes[0])
				}
			}
			entries = append(entries, item)
		}
		return entries
	}
	charEntries := parse(hongguoSTCharacters)
	variantEntries := parse(hongguoTWVariants)
	keys := make(map[rune]bool, len(charEntries))
	mapping := make(map[rune]rune, len(charEntries)*2)
	for _, item := range charEntries {
		keys[item.key] = true
		mapping[item.key] = item.key
	}
	for _, item := range charEntries {
		for _, value := range item.values {
			if keys[value] {
				continue
			}
			if _, exists := mapping[value]; !exists {
				mapping[value] = item.key
			}
		}
	}
	for _, item := range variantEntries {
		canonical := item.key
		if mapped, ok := mapping[item.key]; ok {
			canonical = mapped
		}
		for _, value := range item.values {
			if keys[value] {
				continue
			}
			if _, exists := mapping[value]; !exists {
				mapping[value] = canonical
			}
		}
	}
	return mapping
}

func hongguoSimplifiedRune(value rune) rune {
	if simplified, ok := hongguoVariantToSimplified[value]; ok {
		return simplified
	}
	return value
}
