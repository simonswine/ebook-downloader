package donaukurier

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/simonswine/ebook-downloader/meta"
)

const (
	htmlTagStart = 60 // Unicode `<`
	htmlTagEnd   = 62 // Unicode `>`
)

// Aggressively strips HTML tags from a string.
// It will only keep anything between `>` and `<`.
func stripHtmlTags(s string) string {
	// Setup a string builder and allocate enough memory for the new string.
	var builder strings.Builder
	builder.Grow(len(s) + utf8.UTFMax)

	in := false // True if we are inside an HTML tag.
	start := 0  // The index of the previous start tag character `<`
	end := 0    // The index of the previous end tag character `>`

	for i, c := range s {
		// If this is the last character and we are not in an HTML tag, save it.
		if (i+1) == len(s) && end >= start {
			builder.WriteString(s[end:])
		}

		// Keep going if the character is not `<` or `>`
		if c != htmlTagStart && c != htmlTagEnd {
			continue
		}

		if c == htmlTagStart {
			// Only update the start if we are not in a tag.
			// This make sure we strip out `<<br>` not just `<br>`
			if !in {
				start = i

				// Write the valid string between the close and start of the two tags.
				builder.WriteString(s[end:start])
			}
			in = true
			continue
		}
		// else c == htmlTagEnd
		in = false
		end = i + 1
	}
	s = builder.String()
	return s
}

// publicationContentItems represents the GetPublicationContentItems JSON response.
type publicationContentItems struct {
	PublicationID int `json:"PublicationID"`
	Content       []struct {
		Category   string `json:"Category"`
		PageNumber int    `json:"PageNumber"`
		ContentItem []struct {
			Title string `json:"Title"`
		} `json:"ContentItem"`
	} `json:"Content"`
}

// parseTocV2 parses bookmarks from the twipecloud GetPublicationContentItems JSON format.
func parseTocV2(r io.Reader) ([]meta.Bookmark, error) {
	var body publicationContentItems
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	var (
		lastCategory string
		result       []meta.Bookmark
	)
	for _, c := range body.Content {
		if c.PageNumber == 0 {
			continue
		}

		if c.Category != lastCategory && c.Category != "" {
			result = append(result, meta.Bookmark{
				PageNumber: c.PageNumber,
				Title:      c.Category,
				Level:      1,
			})
			lastCategory = c.Category
		}

		for _, item := range c.ContentItem {
			title := strings.TrimSpace(stripHtmlTags(item.Title))
			if title == "" {
				continue
			}
			result = append(result, meta.Bookmark{
				PageNumber: c.PageNumber,
				Title:      title,
				Level:      2,
			})
		}
	}

	return result, nil
}


