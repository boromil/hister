// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"time"
)

// DocumentVersion stores unified text diffs of a document's HTML and plain
// text fields captured each time the document is re-indexed and its URL
// matches a versioning rule. Either field may be empty when the corresponding
// content was absent or unchanged.
type DocumentVersion struct {
	ID        uint      `gorm:"primaryKey"                json:"id"`
	CreatedAt time.Time `gorm:"index:idx_version_url_user" json:"created_at"`
	URL       string    `gorm:"index:idx_version_url_user" json:"url"`
	UserID    uint      `gorm:"index:idx_version_url_user" json:"user_id"`
	HTMLDiff  string    `gorm:"type:text"                  json:"html_diff"`
	TextDiff  string    `gorm:"type:text"                  json:"text_diff"`
}

// SaveDocumentVersion creates a new version entry for the given URL and user.
func SaveDocumentVersion(url string, userID uint, htmlDiff, textDiff string) error {
	v := &DocumentVersion{
		URL:      url,
		UserID:   userID,
		HTMLDiff: htmlDiff,
		TextDiff: textDiff,
	}
	return DB.Create(v).Error
}

// GetDocumentVersions returns all stored version diffs for a URL and user,
// ordered from newest to oldest.
func GetDocumentVersions(url string, userID uint) ([]*DocumentVersion, error) {
	var versions []*DocumentVersion
	if err := DB.Where("url = ? AND user_id = ?", url, userID).
		Order("created_at DESC").
		Find(&versions).Error; err != nil {
		return nil, err
	}
	return versions, nil
}
