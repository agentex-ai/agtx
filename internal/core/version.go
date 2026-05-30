package core

import (
	"strconv"
	"strings"
)

func compareVersion(a, b string) int {
	aParts := versionParts(a)
	bParts := versionParts(b)
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for index := 0; index < maxLen; index++ {
		var aPart, bPart int
		if index < len(aParts) {
			aPart = aParts[index]
		}
		if index < len(bParts) {
			bPart = bParts[index]
		}
		if aPart < bPart {
			return -1
		}
		if aPart > bPart {
			return 1
		}
	}
	return strings.Compare(a, b)
}

func versionParts(version string) []int {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	fields := strings.FieldsFunc(version, func(r rune) bool {
		return r == '.' || r == '-' || r == '+'
	})
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		if field == "" {
			continue
		}
		value, err := strconv.Atoi(field)
		if err != nil {
			break
		}
		parts = append(parts, value)
	}
	return parts
}
