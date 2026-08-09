package main

import (
	"flag"
	"log"
	"net/http"
	"sync"
	"time"
)

func main() {
	/*
		コマンドが利用するフラグの事前定義
		以下のformatが有効
		・-flag
		・--flag   // double dashes are also permitted
		・-flag=x
		・-flag x  // non-boolean flags only
	*/

	// 数値n, 文字列urlを実行時にフラグで待ち受ける
	n := flag.Int("n", 10, "並列数")
	url := flag.String("url", "http://localhost:8080/healthz", "リクエスト先")
	// 全てのフラグが宣言され、プログラム内でアクセスされるまでの間にParse()が必要
	flag.Parse()

	// 「内部に保持するカウンタ（stateフィールド）が0になるまで、処理をブロックする」仕組みを提供する型
	// 状態を保持するので、使い回す際は必ず参照渡しする
	var wg sync.WaitGroup
	for i := range *n {
		// 自身のカウンタへdelta（ここでは1、負数もOK）を加算
		// goroutine外で呼ぶ（goroutineは非同期なので、中で呼ぶとAdd()実行前にWait()に達し、待たずに終了しうる）
		wg.Add(1)
		go func() {
			// 自身のカウンタから-1する->Add(-1)のエイリアス
			// deferで呼ぶのが慣例（goroutine完了後に必ずカウンタをデクリメントしないと、Wait()が永久ブロックに陥るため）
			defer wg.Done()
			start := time.Now()
			resp, err := http.Get(*url)
			if err != nil {
				log.Printf("[%d] error after %v: %v", i, time.Since(start), err)
				return
			}
			defer resp.Body.Close()
			log.Printf("[%d] status=%d elapsed=%v", i, resp.StatusCode, time.Since(start))
		}()

		/*
			カウンタ処理とgo文は、wg.Go(f)で置き換えられる（糖衣構文）
			↓は↑のfor文内と等価で、「wg.Add(1) - >go func(){ defer wg.Done() }()」を一行で書ける
				wg.Go(func() {
					start := time.Now()
					resp, err := http.Get(*url)
					...
				})
		*/
	}
	// 自身のカウンタが0になるまで呼び出し元をブロックする
	// カウンタの値とDone()の回数が釣り合わない場合、ブロックを解放できなくなって"all goroutines are asleep - deadlock!"
	// - ex. ↑でAdd(2)を試すとfatal error
	wg.Wait()
}
