package db

import (
	"fmt"
	"time"
)

// TimeLayout は DB に保存する日時の書式（UTC・秒精度・固定長）。
// 固定長にすることで、文字列比較（ORDER BY や CHECK 制約）が時刻の大小と一致する。
const TimeLayout = "2006-01-02T15:04:05Z"

// FormatTime は日時を DB 保存用の文字列に変換する。
func FormatTime(t time.Time) string {
	return t.UTC().Format(TimeLayout)
}

// ParseTime は DB に保存された日時文字列を time.Time に変換する。
func ParseTime(s string) (time.Time, error) {
	t, err := time.Parse(TimeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("日時 %q の解析に失敗: %w", s, err)
	}
	return t, nil
}
