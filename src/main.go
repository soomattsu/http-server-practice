package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
)

type Document struct {
	Author string `json:"author"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type DocStorage struct {
	documents map[string]Document
	mu        sync.RWMutex
}

var dStorage = DocStorage{
	documents: map[string]Document{
		"1": {"Tanaka", "First one yeahhhh", "public"},
		"2": {"Sato", "Broken doc foo bar baz", "private"},
		"3": {"Nagano", "Can we do something? Yes we can!", "public"},
	},
	mu: sync.RWMutex{},
}

func healthzHandler() func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}

// グローバルなデータストアからDocument一覧を取得してJSONエンコードし、bodyとして返す
func getDocumentsHandler() func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		dStorage.mu.RLock()
		defer dStorage.mu.RUnlock()
		ds := make([]Document, 0, len(dStorage.documents))
		for _, d := range dStorage.documents {
			ds = append(ds, d)
		}
		documentsJson, err := json.Marshal(ds)
		if err != nil { // デコード失敗時は500を返す （ex. json.Marshal(make(chan int))）
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(documentsJson)
	}
}

// グローバルなデータストアから指定されたidのDocumentを取得してJSONエンコードし、bodyとして返す
func getDocumentByIdHandler() func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		docId := req.PathValue("id")
		dStorage.mu.RLock()
		defer dStorage.mu.RUnlock()
		doc, ok := dStorage.documents[docId]
		if !ok {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		docJson, err := json.Marshal(doc)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(docJson)
	}
}

// req bodyのJSON文字列をデコードし、新規Documentとしてグローバルなデータストアに追加する
func postDocumentHandler() func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		// req body取得
		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		// JSON文字列をデコード
		var doc Document
		err = json.Unmarshal(body, &doc)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		// 採番して格納
		dStorage.mu.Lock()
		defer dStorage.mu.Unlock()
		docId := strconv.Itoa(len(dStorage.documents) + 1)
		dStorage.documents[docId] = doc
		w.Header().Set("Location", "/documents/"+docId)
		w.WriteHeader(http.StatusCreated)
	}
}

func main() {
	// 任意のpathに対応するhandler関数を"DefaultServeMux"へ登録（ルーティング設定）
	// - DefaultServeMux: net/httpパッケージにあらかじめ用意されている、グローバルなマルチプレクサ（ServeMux）のインスタンス
	// - マルチプレクサ(Mux): N個の入力から、選択信号を元に、1個の出力を返す機構
	//   - 複数の入力(pattern, handler)から、選択信号(req)を元に、1個の出力（res）を返す機構
	http.HandleFunc("/healthz", healthzHandler())
	http.HandleFunc("GET /documents", getDocumentsHandler())
	http.HandleFunc("POST /documents", postDocumentHandler())
	http.HandleFunc("GET /documents/{id}", getDocumentByIdHandler())

	// http serverの起動。2nd argがnilなので、ルーティングにはDefaultServeMuxが使われる
	log.Fatal(http.ListenAndServe(":8080", nil))
}
