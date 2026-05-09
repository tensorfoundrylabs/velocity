package pretty

import (
	velocity "github.com/tensorfoundrylabs/velocity"
)

// The Result type aliases below point at canonical types in the root package.
// They exist only to keep the pretty package compiling during Phase 1a.
// Phase 1b removes the pretty package entirely.

// BoxResult is an alias for velocity.Box.
type BoxResult = velocity.Box

// NewBoxResult forwards to velocity.NewBox.
func NewBoxResult(title, content string, theme *velocity.Theme) *BoxResult {
	return velocity.NewBox(title, content, theme)
}

// TableResult is an alias for velocity.Table.
type TableResult = velocity.Table

// NewTableResult forwards to velocity.NewTable.
func NewTableResult(headers []string, rows [][]string, theme *velocity.Theme) *TableResult {
	return velocity.NewTable(headers, rows, theme)
}

// BannerResult is an alias for velocity.Banner.
type BannerResult = velocity.Banner

// NewBannerResult forwards to velocity.NewBanner.
func NewBannerResult(text string, theme *velocity.Theme) *BannerResult {
	return velocity.NewBanner(text, theme)
}

// TreeResult is an alias for velocity.Tree.
type TreeResult = velocity.Tree

// NewTreeResult converts pretty.TreeItem nodes to velocity.TreeItem and forwards
// to velocity.NewTree.
func NewTreeResult(nodes []TreeItem, theme *velocity.Theme) *TreeResult {
	return velocity.NewTree(convertTreeItems(nodes), theme)
}

// KeyValueResult is an alias for velocity.KeyValue.
type KeyValueResult = velocity.KeyValue

// NewKeyValueResult forwards to velocity.NewKeyValue.
func NewKeyValueResult(key, value string, theme *velocity.Theme) *KeyValueResult {
	return velocity.NewKeyValue(key, value, theme)
}

// SystemInfoResult is an alias for velocity.SystemInfo.
type SystemInfoResult = velocity.SystemInfo

// NewSystemInfoResult converts the pretty-local SystemInfo data struct to the root
// SystemInfoData type and forwards to velocity.NewSystemInfo.
func NewSystemInfoResult(info *SystemInfo, theme *velocity.Theme) *SystemInfoResult {
	if info == nil {
		return velocity.NewSystemInfo(nil, theme)
	}
	data := &velocity.SystemInfoData{
		Title:   info.Title,
		Version: info.Version,
		Fields:  make([]velocity.KeyValuePair, len(info.Fields)),
	}
	for i, f := range info.Fields {
		data.Fields[i] = velocity.KeyValuePair{Key: f.Key, Value: f.Value}
	}
	return velocity.NewSystemInfo(data, theme)
}

// convertTreeItems maps pretty.TreeItem to velocity.TreeItem recursively.
func convertTreeItems(items []TreeItem) []velocity.TreeItem {
	result := make([]velocity.TreeItem, len(items))
	for i, item := range items {
		result[i] = velocity.TreeItem{
			Key:      item.Key,
			Value:    item.Value,
			Children: convertTreeItems(item.Children),
		}
	}
	return result
}
