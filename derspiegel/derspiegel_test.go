package derspiegel

import (
	"testing"

	"github.com/simonswine/ebook-downloader/meta"
	"github.com/stretchr/testify/require"
)

// Test_ShouldDownloadIssue tests the logic for determining whether an issue should be downloaded
// This encapsulates the nil-safety logic from the sync command
func Test_ShouldDownloadIssue(t *testing.T) {
	tests := []struct {
		name         string
		lastBook     *meta.Info
		issue        *meta.Info
		wantDownload bool
	}{
		{
			name:         "no books in database - should download",
			lastBook:     nil,
			issue:        &meta.Info{Issue: intPtr(10)},
			wantDownload: true,
		},
		{
			name:         "lastBook exists but Issue is nil - should download",
			lastBook:     &meta.Info{Issue: nil},
			issue:        &meta.Info{Issue: intPtr(10)},
			wantDownload: true,
		},
		{
			name:         "issue is newer than lastBook - should download",
			lastBook:     &meta.Info{Issue: intPtr(5)},
			issue:        &meta.Info{Issue: intPtr(10)},
			wantDownload: true,
		},
		{
			name:         "issue is same as lastBook - should not download",
			lastBook:     &meta.Info{Issue: intPtr(10)},
			issue:        &meta.Info{Issue: intPtr(10)},
			wantDownload: false,
		},
		{
			name:         "issue is older than lastBook - should not download",
			lastBook:     &meta.Info{Issue: intPtr(15)},
			issue:        &meta.Info{Issue: intPtr(10)},
			wantDownload: false,
		},
		{
			name:         "issue number is nil - should download",
			lastBook:     &meta.Info{Issue: intPtr(10)},
			issue:        &meta.Info{Issue: nil},
			wantDownload: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldDownloadIssue(tt.lastBook, tt.issue)
			require.Equal(t, tt.wantDownload, got)
		})
	}
}

// intPtr is a helper function to create int pointers
func intPtr(i int) *int {
	return &i
}
