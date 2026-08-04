package main

import (
	"log"
	"net/http"

	"github.com/soomattsu/http-server-practice/internal/handler"
)

func main() {
	// 任意のpathに対応するhandler関数を"DefaultServeMux"へ登録（ルーティング設定）
	// - DefaultServeMux: net/httpパッケージにあらかじめ用意されている、グローバルなマルチプレクサ（ServeMux）のインスタンス
	// - マルチプレクサ(Mux): N個の入力から、選択信号を元に、1個の出力を返す機構
	//   - 複数の入力(pattern, handler)から、選択信号(req)を元に、1個の出力（res）を返す機構
	http.HandleFunc("/healthz", handler.HealthzHandler)
	http.HandleFunc("GET /documents", handler.GetDocumentsHandler)
	http.HandleFunc("POST /documents", handler.PostDocumentHandler)
	http.HandleFunc("GET /documents/{id}", handler.GetDocumentByIdHandler)

	// http serverの起動。2nd argがnilなので、ルーティングにはDefaultServeMuxが使われる
	log.Fatal(http.ListenAndServe(":8080", nil))
}
