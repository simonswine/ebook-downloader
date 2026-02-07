package derspiegel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/simonswine/ebook-downloader/meta"
	"github.com/stretchr/testify/require"
)

func updateTXT(t testing.TB, name string, p tocParser) {
	txtPath := filepath.Join("testdata", name)
	pdfPath := txtPath[0:len(txtPath)-4] + ".pdf"
	_, err := os.Stat(pdfPath)
	if os.IsNotExist(err) {
		t.Logf("Skip updating %s as %s doesn't exist", name, pdfPath)
		return
	}
	require.NoError(t, err)

	f, err := os.Create(txtPath)
	require.NoError(t, err)
	defer f.Close()

	require.NoError(t, p.extractText(pdfPath, f))
}

func Test_parseToc(t *testing.T) {
	// Read the test files from the "testdata" folder
	files, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("failed to read testdata folder: %v", err)
	}

	for _, file := range files {
		if !strings.HasPrefix(file.Name(), "table-of-contents") || !strings.HasSuffix(file.Name(), ".txt") {
			continue
		}
		name := file.Name()
		t.Run(name, func(t *testing.T) {
			// read the file
			f, err := os.Open(filepath.Join("testdata", name))
			require.NoError(t, err)

			issue, err := strconv.ParseInt(name[len(name)-6:len(name)-4], 10, 64)
			require.NoError(t, err)
			issueI := int(issue)

			year, err := strconv.ParseInt(name[len(name)-11:len(name)-7], 10, 64)
			require.NoError(t, err)
			yearI := int(year)

			info := meta.Info{
				Issue: &issueI,
				Year:  &yearI,
			}

			// Parse the TOC
			p := getTocParser(&info)

			// Update text version when pdf is available
			if os.Getenv("UPDATE_TXT") == "true" {
				updateTXT(t, name, p)
			}

			toc, err := p.parseTOC(f)
			require.NoError(t, err)

			actual, err := json.Marshal(toc)
			require.NoError(t, err)

			if os.Getenv("UPDATE_JSON") == "true" {
				formatted, err := json.MarshalIndent(toc, "", "  ")
				require.NoError(t, err)
				err = os.WriteFile(filepath.Join("testdata", file.Name()+".json"), formatted, 0644)
				require.NoError(t, err)
				return
			}

			// read the expected file
			expected, err := os.ReadFile(filepath.Join("testdata", file.Name()+".json"))
			require.NoError(t, err)

			require.JSONEq(t, string(expected), string(actual))
		})
	}
}
