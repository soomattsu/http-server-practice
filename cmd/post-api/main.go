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

	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"

	"github.com/soomattsu/http-server-practice/internal/platform"
	"github.com/soomattsu/http-server-practice/internal/post/handler"
	"github.com/soomattsu/http-server-practice/internal/post/repository"
	"github.com/soomattsu/http-server-practice/internal/post/service"
)

func main() {
	cfg := platform.LoadCfg()

	// Datadog用環境変数（DD_ENV/DD_SERVICE/DD_VERSION etc）を自動的に読み込む
	// デフォルトの送信先はlocalhost:8126（datadog-agent sidecar）で、同一Task内のコンテナなので繋がる
	if err := tracer.Start(); err != nil {
		log.Printf("Failed to start datadog tracer: %v", err)
	}
	defer tracer.Stop()

	// *gorm.DB(*sql.DB)の寿命＝プロセスの寿命というライブラリ側設計なので、通常のサーバーアプリでは明示的にCloseしなくていい
	// - The returned DB is safe for concurrent use by multiple goroutines and maintains its own pool of idle connections. Thus, the Open function should be called just once. It is rarely necessary to close a DB.
	// 逆に、プロセスの寿命 > コネクションプールの寿命なら、明示的にCloseしないとleakが生じる
	db, err := platform.InitMySQL(cfg)
	if err != nil {
		log.Fatalf("Failed to init MySQL: %v", err)
	}
	if err := repository.Seed(db); err != nil {
		log.Fatalf("Failed to seed data: %v", err)
	}
	postRepo := repository.NewPostRepo(db)

	// *redis.Clientの寿命＝プロセスの寿命というライブラリ側設計なので、通常のサーバーアプリでは明示的にCloseしなくていい
	// - It is rare to Close a Client, as the Client is meant to be long-lived and shared between many goroutines.
	// 逆に、プロセスの寿命 > コネクションプールの寿命なら、明示的にCloseしないとleakが生じる
	var postService *service.PostService
	if kvs, err := platform.InitRedis(cfg); err != nil {
		// Redisは任意依存として、接続できなければcache無しで起動を継続する
		log.Printf("Failed to init Redis, running without cache: %v", err)
		postService = service.NewPostService(postRepo, repository.NewNoopPostCache())
	} else {
		postService = service.NewPostService(postRepo, repository.NewPostCache(kvs))
	}

	postHandler := handler.NewPostHandler(postService)
	router := handler.NewRouter(postHandler)

	// http serverの初期化。
	// タイムアウトはコネクションごとに有効
	s := &http.Server{
		Addr: ":8080",
		// 自前のServeMux（計装済）を挿入。nilの場合、ルーティングにはDefaultServeMuxが使われる
		Handler: router,
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
