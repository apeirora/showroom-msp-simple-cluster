/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package pathwalk resolves dotted JSON-like paths with optional list
// indexing ("status.lastErrors[0].description") against arbitrary
// map[string]any objects.
package pathwalk

import (
	"fmt"
	"strconv"
	"strings"
)

// Walk resolves path against m. Returns the value at the path and true on
// success; nil, false otherwise.
func Walk(m map[string]any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		key, idx, hasIdx := splitIndex(part)
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		val, ok := mm[key]
		if !ok {
			return nil, false
		}
		if !hasIdx {
			cur = val
			continue
		}
		arr, ok := val.([]any)
		if !ok || idx < 0 || idx >= len(arr) {
			return nil, false
		}
		cur = arr[idx]
	}
	return cur, true
}

// String resolves path and stringifies the result via ToString.
func String(m map[string]any, path string) string {
	v, ok := Walk(m, path)
	if !ok {
		return ""
	}
	return ToString(v)
}

// ToString renders a JSON-decoded value as a string for human display.
func ToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func splitIndex(part string) (key string, idx int, hasIdx bool) {
	open := strings.IndexByte(part, '[')
	if open < 0 || !strings.HasSuffix(part, "]") {
		return part, 0, false
	}
	n, err := strconv.Atoi(part[open+1 : len(part)-1])
	if err != nil {
		return part, 0, false
	}
	return part[:open], n, true
}
