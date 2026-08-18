package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// NewRouter はすべてのルーティングを統合したServeMuxを返す。
// TODO: 他のモデル用handlerが増えるなら、引数を抽象化する（今はPostのみ）
func NewRouter(post *PostHandler) http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("/healthz", healthz)
	router.HandleFunc("/superslow", superslow)
	// router = RegisterPhase1(router)
	router = post.Register(router)
	return router
}

// healthz はAPIのヘルスチェックのために呼ばれ、常に200を返す。
func healthz(w http.ResponseWriter, _ *http.Request) {
	raw := map[string]string{"status": "OK"}
	res, _ := json.Marshal(raw)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(res)
}

func superslow(w http.ResponseWriter, r *http.Request) {
	log.Println("start some slow procedure")
	time.Sleep(30 * time.Second)
	log.Println("complete some slow procedure")
	w.WriteHeader(200)
}
