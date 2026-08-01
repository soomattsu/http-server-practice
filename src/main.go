package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
)

type Document struct {
	Id     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type DocStorage struct {
	documents map[string]Document // keyはidを想定
	mu        sync.RWMutex
}

var (
	d1       = Document{"1", "First one yeahhhh", "v1"}
	d2       = Document{"2", "Broken doc foo bar baz", "v2"}
	d3       = Document{"3", "Can we do something? Yes we can!", "v2"}
	dList    = []Document{d1, d2, d3}
	dMap     = map[string]Document{"1": d1, "2": d2, "3": d3}
	dStorage = DocStorage{documents: dMap}
)

func healthzHandler() func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}

func getDocumentsHandler() func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		documentsJson, err := json.Marshal(dList)
		// documentsJson, err = json.Marshal(make(chan int)) <- 500 errorが返るケース
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(documentsJson)
	}
}

func main() {
	// handler関数の初期化
	helloHandler := func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, "Hello, World from net/http!\n")
	}

	// 任意のpathに対応するhandler関数を"DefaultServeMux"へ登録（ルーティング設定）
	// - DefaultServeMux: net/httpパッケージにあらかじめ用意されている、グローバルなマルチプレクサ（ServeMux）のインスタンス
	// - マルチプレクサ(Mux): N個の入力から、選択信号を元に、1個の出力を返す機構
	//   - 複数の入力(pattern, handler)から、選択信号(req)を元に、1個の出力（res）を返す機構
	http.HandleFunc("/", helloHandler)
	http.HandleFunc("/healthz", healthzHandler())
	http.HandleFunc("GET /documents", getDocumentsHandler())

	// http serverの起動。2nd argがnilなので、ルーティングにはDefaultServeMuxが使われる
	log.Fatal(http.ListenAndServe(":8080", nil))
}
