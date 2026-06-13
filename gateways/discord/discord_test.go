package discord

import (
	"strings"
	"testing"
)

func TestSplitMessage(t *testing.T) {
	// 1. Text within limit should not be split
	shortText := "Hello, short message!"
	chunks := splitMessage(shortText, 50)
	if len(chunks) != 1 || chunks[0] != shortText {
		t.Errorf("Expected 1 chunk matching input, got %d: %v", len(chunks), chunks)
	}

	// 2. Text splitting at paragraph boundary
	longTextWithParagraphs := "First paragraph of some length.\n\nSecond paragraph of some length. Here is another sentence."
	// Limit of 60 characters:
	// "First paragraph of some length.\n\n" is 33 chars.
	// Splitting on paragraph should capture the first block.
	chunks = splitMessage(longTextWithParagraphs, 60)
	if len(chunks) != 2 {
		t.Fatalf("Expected 2 chunks, got %d: %v", len(chunks), chunks)
	}
	if chunks[0] != "First paragraph of some length.\n\n" {
		t.Errorf("Expected first chunk to split at paragraph, got %q", chunks[0])
	}
	if chunks[1] != "Second paragraph of some length. Here is another sentence." {
		t.Errorf("Expected second chunk to capture remaining, got %q", chunks[1])
	}

	// 3. Fallback to space split if no paragraph/newline is available
	longTextWithSpaces := "One two three four five six seven eight nine ten eleven twelve"
	// Limit of 15 characters.
	// "One two three " is 14 characters. Next word is "four".
	// Expected split at the space after "three".
	chunks = splitMessage(longTextWithSpaces, 15)
	if len(chunks) < 2 {
		t.Fatalf("Expected multiple chunks, got %d: %v", len(chunks), chunks)
	}
	if !strings.HasPrefix(chunks[0], "One two three ") {
		t.Errorf("Expected split on space boundary, first chunk got %q", chunks[0])
	}

	// 4. Exact split when no whitespaces are present
	noSpaces := "abcdefghijklmnopqrstuvwxyz"
	chunks = splitMessage(noSpaces, 10)
	if len(chunks) != 3 {
		t.Fatalf("Expected 3 chunks, got %d: %v", len(chunks), chunks)
	}
	if chunks[0] != "abcdefghij" || chunks[1] != "klmnopqrst" || chunks[2] != "uvwxyz" {
		t.Errorf("Expected exact character splits, got %v", chunks)
	}
}
