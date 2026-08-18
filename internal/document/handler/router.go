package handler

import (
	"encoding/json"
	"net/http"
)

// NewRouter はすべてのルーティングを統合したServeMuxを返す。
func NewRouter() http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("/healthz", healthz)
	router = RegisterDocument(router)
	return router
}

// healthz はAPIのヘルスチェックのために呼ばれ、常に200を返す。
func healthz(w http.ResponseWriter, _ *http.Request) {
	raw := map[string]string{"status": "OK"}
	res, _ := json.Marshal(raw)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(res)
}
