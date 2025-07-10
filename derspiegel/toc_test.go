package derspiegel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_parseToc(t *testing.T) {
	// read the test files from the testdata folder
	files, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("failed to read testdata folder: %v", err)
	}

	for _, file := range files {
		if !strings.HasPrefix(file.Name(), "table-of-contents") || !strings.HasSuffix(file.Name(), ".txt") {
			continue
		}
		t.Run(file.Name(), func(t *testing.T) {
			// read the file
			f, err := os.Open(filepath.Join("testdata", file.Name()))
			require.NoError(t, err)
			defer f.Close()

			// parse the toc
			toc, err := parseTOC(f)
			require.NoError(t, err)

			actual, err := json.Marshal(toc)
			require.NoError(t, err)

			if os.Getenv("UPDATE") == "true" {
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
