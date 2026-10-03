module github.com/kawafuchieirin/task-manager/insight

go 1.27.1

require github.com/kawafuchieirin/task-manager/shared v0.0.0

// 同じリポジトリの共通モジュール。go.work が無い環境（単体ビルド）でも解決できるようにする。
replace github.com/kawafuchieirin/task-manager/shared => ../shared
