package donaukurier

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/simonswine/ebook-downloader/meta"
)

type pageContent struct {
	PageWebReader int `json:"pageWebreader"`
	Content       struct {
		Ressort string `json:"ressort"`
		Title   string `json:"title"`
		Date    string `json:"date"`
	}
}

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

func parseToc(r io.Reader) ([]meta.Bookmark, error) {
	body := make(map[string][]pageContent)
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	// order pages correctly
	keys := make([][]pageContent, len(body))
	for k, v := range body {
		page, err := strconv.Atoi(k)
		if err != nil {
			return nil, fmt.Errorf("failed to convert key to int: %w", err)
		}

		keys[page-1] = v
	}

	var (
		lastRessort string
		result      []meta.Bookmark
	)
	for idx, v := range keys {
		if idx == 0 {
			continue
		}

		for _, v := range v {
			if v.Content.Title == "" {
				continue
			}

			if v.Content.Ressort != lastRessort && v.Content.Ressort != "" {
				result = append(result, meta.Bookmark{
					PageNumber: idx + 1,
					Title:      stripHtmlTags(v.Content.Ressort),
					Level:      1,
				})
				lastRessort = v.Content.Ressort
			}

			result = append(result, meta.Bookmark{
				PageNumber: idx + 1,
				Title:      stripHtmlTags(v.Content.Title),
				Level:      2,
			})
		}
	}

	return result, nil
}
