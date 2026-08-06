package handler

import (
	"encoding/json"
	"net/http"
)

// Healthz はAPIのヘルスチェックのために呼ばれ、常に200を返す。
func Healthz(w http.ResponseWriter, _ *http.Request) {
	raw := map[string]string{"status": "OK"}
	res, _ := json.Marshal(raw) // rawが固定値の間はerror処理不要
	// if err != nil {
	// 	log.Printf("Server error: %v", err)
	// 	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	// 	return
	// }
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(res)
}
