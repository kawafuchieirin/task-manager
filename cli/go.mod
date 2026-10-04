module github.com/kawafuchieirin/task-manager/cli

go 1.27.1

require github.com/kawafuchieirin/task-manager/client v0.0.0

// 同じリポジトリの API クライアント。go.work が無い環境（単体ビルド）でも解決できるようにする。
replace github.com/kawafuchieirin/task-manager/client => ../client
