module github.com/kawafuchieirin/task-manager/api

go 1.27.1

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.48.0 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

require (
	github.com/kawafuchieirin/task-manager/shared v0.0.0
	modernc.org/sqlite v1.60.1
)

// 同じリポジトリの共通モジュール。go.work が無い環境（単体ビルド）でも解決できるようにする。
replace github.com/kawafuchieirin/task-manager/shared => ../shared
