package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/soomattsu/http-server-practice/internal/handler"
)

func main() {
	// 任意のpathに対応するhandler関数を"DefaultServeMux"へ登録（ルーティング設定）
	// - DefaultServeMux: net/httpパッケージにあらかじめ用意されている、グローバルなマルチプレクサ（ServeMux）のインスタンス
	// - マルチプレクサ(Mux): N個の入力から、選択信号を元に、1個の出力を返す機構
	//   - 複数の入力(pattern, handler)から、選択信号(req)を元に、1個の出力（res）を返す機構
	http.HandleFunc("/healthz", handler.Healthz)
	http.HandleFunc("GET /documents", handler.GetDocuments)
	http.HandleFunc("POST /documents", handler.PostDocument)
	http.HandleFunc("GET /documents/{id}", handler.GetDocumentByID)
	http.HandleFunc("/superslow", func(w http.ResponseWriter, r *http.Request) {
		log.Println("start some slow procedure")
		time.Sleep(30 * time.Second)
		log.Println("complete some slow procedure")
		w.WriteHeader(200)
	})

	// http serverの初期化。Handlerフィールドがnilなので、ルーティングにはDefaultServeMuxが使われる
	// タイムアウトはコネクションごとに有効
	s := &http.Server{
		Addr: ":8080",
		// req headerの読み込みに許される時間
		ReadHeaderTimeout: 1 * time.Second,
		// req全体の読み込みに許される時間
		// 一律なので、（データアップロードを受けるなど）handler毎に個別にtimeoutを切りたい場合はReadHeaderTimeout+handler側タイムアウトなどで制御するべき
		ReadTimeout: 5 * time.Second,
		// （header読み込み完了時点から数えて）body読み込み→handler処理→res書き込みに許される時間
		// これを超過してもhandlerが止まったりはしないが、handler実行結果をresに書き込もうとすると即エラーになる
		WriteTimeout: 10 * time.Second,
		// keep-aliveが有効な時に、req1書き込み完了→req2読み込み開始までの待機時間の上限
		IdleTimeout: 60 * time.Second,
	}

	// 指定したsignalを受け取る(+stopが呼ばれる or 1st argのctxがDoneされる)とDoneされるctxを返す
	// Doneを使った受信待ちブロック（<-ctx.Done()）を書くことで「任意のシグナルを受け取るまで実行されない処理」を記述できる
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// stop()は、書き換えたsignal用の振る舞いをデフォルトに戻す
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		// s.Shutdown()発火時、ListenAndServe()はErrServerClosedを返すが、これは正常終了
		// その他異常系のみ、errChでmain goroutineへ送信
		if err := s.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-sigCtx.Done():
		log.Println("Graceful shutdown: started")
		// ECS/K8sなどplatform側のgracePeriodより短く設定することで、外からSIGKILLされる前にログが出せる
		sdCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := s.Shutdown(sdCtx); err != nil {
			log.Printf("Graceful shutdown: error %v", err)
		}
	case err := <-errCh:
		log.Fatalf("HTTP server ListenAndServe failed: %v", err)
	}
}
