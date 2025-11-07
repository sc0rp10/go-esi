package esi

import (
	"net/http"
)

type (
	Tag interface {
		Process([]byte, *http.Request) ([]byte, int)
		HasClose([]byte) bool
		GetClosePosition([]byte) int
	}

	baseTag struct {
		length int
	}

	includeRequest struct {
		tag      *includeTag
		position int
		length   int
	}

	includeResult struct {
		content  []byte
		position int
		length   int
	}
)

func newBaseTag() *baseTag {
	return &baseTag{length: 0}
}

func (b *baseTag) Process(content []byte, _ *http.Request) ([]byte, int) {
	return []byte{}, len(content)
}
