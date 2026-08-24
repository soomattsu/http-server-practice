package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	httptrace "github.com/DataDog/dd-trace-go/contrib/net/http/v2"
)

// NewRouter はすべてのルーティングを統合したServeMux（計装済み）を返す。
// TODO: 他のモデル用handlerが増えるなら、引数を抽象化する（今はPostのみ）
func NewRouter(post *PostHandler) http.Handler {
	// span作成前にルーティングを解決する
	// 通常のServeMuxをWrapHandlerでwrapすると、span作成時にルーティングが確定していないので汎用resource名になってしまう
	router := httptrace.NewServeMux()
	router.HandleFunc("/healthz", healthz)
	router.HandleFunc("/superslow", superslow)
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
