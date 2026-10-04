// Package api は Task API の仕様書（OpenAPI）をバイナリに組み込んで提供する。
// 実装は internal 以下にある（cmd/api が起動する）。
package api

import _ "embed"

// OpenAPI は Task API の仕様書（openapi.yaml）。他のアプリが GET /api/v1/openapi.yaml で取得できる。
//
//go:embed openapi.yaml
var OpenAPI []byte
