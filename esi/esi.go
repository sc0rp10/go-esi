package esi

import (
	"net/http"
	"sync"
)

func findTagName(b []byte) Tag {
	name := tagname.FindSubmatch(b)
	if name == nil {
		return nil
	}

	switch string(name[1]) {
	case comment:
		return &commentTag{
			baseTag: newBaseTag(),
		}
	case choose:
		return &chooseTag{
			baseTag: newBaseTag(),
		}
	case escape:
		return &escapeTag{
			baseTag: newBaseTag(),
		}
	case include:
		return &includeTag{
			baseTag: newBaseTag(),
		}
	case remove:
		return &removeTag{
			baseTag: newBaseTag(),
		}
	case try:
	case vars:
		return &varsTag{
			baseTag: newBaseTag(),
		}
	default:
		return nil
	}

	return nil
}

func HasOpenedTags(b []byte) bool {
	return esi.FindIndex(b) != nil || escapeRg.FindIndex(b) != nil
}

func CanProcess(b []byte) bool {
	if tag := findTagName(b); tag != nil {
		return tag.HasClose(b)
	}

	return false
}

func ReadToTag(next []byte, pointer int) (startTagPosition, esiPointer int, t Tag) {
	var isEscapeTag bool

	tagIdx := esi.FindIndex(next)

	if escIdx := escapeRg.FindIndex(next); escIdx != nil && (tagIdx == nil || escIdx[0] < tagIdx[0]) {
		tagIdx = escIdx
		tagIdx[1] = escIdx[0]
		isEscapeTag = true
	}

	if tagIdx == nil {
		return len(next), 0, nil
	}

	esiPointer = tagIdx[1]
	startTagPosition = tagIdx[0]
	t = findTagName(next[esiPointer:])

	if isEscapeTag {
		esiPointer += 7
	}

	return
}

// Parse parses ESI tags with parallel fetching of includes.
// All includes at the same level are fetched concurrently for optimal performance.
func Parse(b []byte, req *http.Request) []byte {
	return parseParallel(b, req)
}

// parseParallel processes ESI tags with parallel fetching of includes at the same level.
// Strategy: Find all includes, fetch them in parallel, then process other tags.
func parseParallel(b []byte, req *http.Request) []byte {
	// Step 1: Collect all include tags in one pass
	includes := collectIncludes(b)

	// Step 2: Fetch all includes in parallel (if any found)
	if len(includes) > 0 {
		b = fetchIncludesParallel(b, includes, req)
	}

	// Step 3: Process remaining non-include tags sequentially
	b = processNonIncludes(b, req)

	return b
}

// collectIncludes scans the document and collects all include tags
func collectIncludes(b []byte) []includeRequest {
	var includes []includeRequest
	pointer := 0

	for pointer < len(b) {
		next := b[pointer:]
		tagIdx := esi.FindIndex(next)

		if tagIdx == nil {
			break
		}

		esiPointer := tagIdx[1]
		t := findTagName(next[esiPointer:])

		// Only collect include tags
		if includeTag, ok := t.(*includeTag); ok {
			closeIdx := closeInclude.FindIndex(next[esiPointer:])
			if closeIdx != nil {
				tagLength := (tagIdx[1] - tagIdx[0]) + closeIdx[1]
				includes = append(includes, includeRequest{
					tag:      includeTag,
					position: pointer + tagIdx[0],
					length:   tagLength,
				})
			}
		}

		// Move past this tag
		pointer += tagIdx[1] + 1
	}

	return includes
}

// processNonIncludes handles all non-include ESI tags (choose, vars, remove, etc.)
func processNonIncludes(b []byte, req *http.Request) []byte {
	pointer := 0

	for pointer < len(b) {
		var escapeTag bool

		next := b[pointer:]
		tagIdx := esi.FindIndex(next)

		if escIdx := escapeRg.FindIndex(next); escIdx != nil && (tagIdx == nil || escIdx[0] < tagIdx[0]) {
			tagIdx = escIdx
			tagIdx[1] = escIdx[0]
			escapeTag = true
		}

		if tagIdx == nil {
			break
		}

		esiPointer := tagIdx[1]
		t := findTagName(next[esiPointer:])

		if escapeTag {
			esiPointer += 7
		}

		// Skip include tags (already processed)
		if _, ok := t.(*includeTag); ok {
			pointer += tagIdx[0] + tagIdx[1] + 1
			continue
		}

		// Process other tag types
		res, p := t.Process(next[esiPointer:], req)
		esiPointer += p

		b = append(b[:pointer], append(next[:tagIdx[0]], append(res, next[esiPointer:]...)...)...)
		pointer += len(res) + tagIdx[0]
	}

	return b
}

// fetchIncludesParallel fetches all includes concurrently and replaces them in the document.
func fetchIncludesParallel(b []byte, includes []includeRequest, req *http.Request) []byte {
	results := make([]includeResult, len(includes))
	var wg sync.WaitGroup

	// Fetch all includes in parallel
	for i, inc := range includes {
		wg.Add(1)
		go func(index int, incReq includeRequest) {
			defer wg.Done()

			// Extract the tag bytes
			endPos := incReq.position + incReq.length
			if endPos > len(b) {
				endPos = len(b)
			}
			tagBytes := b[incReq.position:endPos]

			// Fetch content
			content := incReq.tag.FetchContent(tagBytes, req)

			results[index] = includeResult{
				content:  content,
				position: incReq.position,
				length:   incReq.length,
			}
		}(i, inc)
	}

	wg.Wait()

	// Replace includes from end to start to maintain positions
	for i := len(results) - 1; i >= 0; i-- {
		res := results[i]
		endPos := res.position + res.length
		if endPos > len(b) {
			endPos = len(b)
		}

		// Replace the include tag with fetched content
		b = append(b[:res.position], append(res.content, b[endPos:]...)...)
	}

	return b
}
