package main

import (
	"io"
	"log"
	"net/http"
)

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

	// http serverの起動
	// 2nd argがnilなので、ルーティングにはDefaultServeMuxが使われる
	log.Fatal(http.ListenAndServe(":8080", nil))
}
