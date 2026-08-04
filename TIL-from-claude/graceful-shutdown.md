# graceful shutdown と http.Server のタイムアウト
Claudeによる学習内容まとめ。
```go
func main() {
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
		log.Println("graceful shutdown: started")
		// ECS/K8sなどplatform側のgracePeriodより短く設定することで、外からSIGKILLされる前にログが出せる
		sdCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := s.Shutdown(sdCtx); err != nil {
			log.Printf("graceful shutdown: error %v", err)
		}
	case err := <-errCh:
		log.Fatalf("HTTP server ListenAndServe failed: %v", err)
	}
}
```
---

## 1. Context の正体

Context は「キャンセルの通知路」。核心は `Done()` が返す `<-chan struct{}` ひとつ。

このチャネルは**値が送られない。close されるだけ**。close 済みチャネルからの受信は即座に返るので：

- キャンセル前：`<-ctx.Done()` はブロックし続ける
- キャンセル後：close され、`<-ctx.Done()` が即座に返る

「1回の close で待っている全員に同時に通知が届く」というチャネルの性質を、そのままキャンセル通知に使っているだけ。`Deadline()` / `Err()` / `Value()` はこの上に乗った付属品。

### 型が `<-chan` である理由

`close` できるのは送信可能なチャネルだけで、受信専用チャネルは `close` できない。つまり `Done() <-chan struct{}` という戻り値の型は、

> 呼び出し側は待てるだけ。close する（＝キャンセルを発火させる）権利は Context の実装側にしかない

という契約を型で表現している。発火手段は `cancel` 関数（`NotifyContext` ならシグナル受信）に限定される。

要素型が `struct{}`（ゼロサイズ）なのも「通知だけで値は運ばない」という意思表示。だから `<-ctx.Done()` は値を受け取らない**文**として書く。

### 木構造

- 根：`context.Background()`（キャンセルされない Context）
- 派生：`context.WithCancel(parent)` / `WithTimeout` / `WithDeadline`

派生関数は `(ctx, cancel)` の2値を返す。`cancel` は関数値で、呼ぶとその ctx と**その子孫すべて**が一斉にキャンセルされる。親がキャンセルされれば子も自動的にキャンセルされる（逆は起きない）。`cancel` を呼ばないと親に紐づいたリソースが残るので `defer cancel()` が定型。

---

## 2. signal.NotifyContext

上の「派生」の一種で、キャンセルのトリガーが**シグナル受信**になっているもの。

```go
sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

内部で `signal.Notify` を張り、指定シグナルが届いた瞬間に ctx をキャンセルする。`stop` はシグナル監視をやめてデフォルト動作に戻す関数。

**効能**：「SIGTERM が来た」が「`<-sigCtx.Done()` が返る」に変換される。シグナル固有の扱い（`chan os.Signal` を自作して `Notify` して `select` して…）を、汎用のキャンセル待ちに落とせる。標準ライブラリの `ExampleServer_Shutdown` は `NotifyContext` が無かった時代の書き方なので、チャネルを自作している。

---

## 3. main の構造：どちらを goroutine に出すか

`ListenAndServe()` も `<-ctx.Done()` もブロックする。よって片方は goroutine に出す。**どちらでもよいが、どちらにしてもチャネルが1本必要になる**。

| 形 | チャネルが必要な理由 |
|---|---|
| `ListenAndServe` を goroutine に | goroutine 内のエラーを main へ運ぶため（`chan error`） |
| `Shutdown` を goroutine に | `Shutdown` の完了を main が待つため（`chan struct{}`） |

後者が標準ライブラリの example の形。前者は起動対象が複数ある場合（HTTP サーバ + メトリクスサーバ + ワーカー等）に自然で、`errgroup` を使う書き方はほぼこの形。今回は前者を採用した。

**判断基準**：goroutine に入れるべきは「戻り値を使わない方」。

**絶対条件**：`main` が return する前に `Shutdown` が完了していること。`main` の return はプロセス終了なので、待たなければ接続を切り捨てて終わる。graceful shutdown を書いているのに graceful でなくなる。

### なぜ `ListenAndServe` の戻り値を捨ててはいけないか

捨てると、**ポート 8080 がすでに使われている**場合に `bind: address already in use` が即座に返って goroutine が終了するが、main は `<-sigCtx.Done()` でブロックし続ける。つまり**サーバが1つも動いていないのにプロセスは生きたまま**になる。ヘルスチェックは通らないが異常終了もしない、という一番厄介な状態。

---

## 4. errCh のバッファと goroutine leak

チャネル送信 `errCh <- err` がいつ完了するか、が全て。

| | 送信の挙動 |
|---|---|
| `make(chan error)` | **受信側が受け取るまでブロック**。受信者がいなければ永久に止まる |
| `make(chan error, 1)` | 空きがあれば**即座に完了**。受信者がいなくてよい |

バッファなし＆フィルタなしだと、SIGTERM 経路でこうなる：

1. main は `case <-sigCtx.Done():` に入る
2. `Shutdown` → `ListenAndServe` が `ErrServerClosed` を返す
3. goroutine が `errCh <- err` を実行
4. **main はもう select を抜けている**。誰も受信しない → goroutine が永久ブロック
5. main が return → プロセス終了 → goroutine ごと消える

5 のせいで**動作上は正常に見える**が、leak している。`main` ではなく普通の関数（`startServer()` 等）の中にあれば、関数が return してもプロセスは生き続けるので goroutine が恒久的に残り、`http.Server` もチャネルも GC されない。

**対策は2通りあり、両方書くのが安全**：

1. **送信側でフィルタ**：`if err := s.ListenAndServe(); !errors.Is(err, http.ErrServerClosed)` — 正常系では送信自体が起きない
2. **バッファ1**：送信が常に即完了するので、受信者の有無に依存しない

1 だけでも安全だが「送信が起きない」ことに依存する。2 も併せると、その推論なしに構造的に leak しなくなる。

---

## 5. ErrServerClosed

`Shutdown` / `Close` が呼ばれると、`ListenAndServe` は**即座に** `http.ErrServerClosed` を返す。「サーバが止まった」という**正常な事実**をエラー型で伝えているだけなので、異常として扱ってはいけない。

センチネルエラーなので `errors.Is(err, http.ErrServerClosed)` で判定する（`==` ではなく。ラップされても拾えるようにするため）。

なお `ListenAndServe` は**常に non-nil のエラーを返す**。nil チェックだけでは正常終了と異常終了を区別できない。

---

## 6. Shutdown の戻り値：経路は2つだけ

`net/http/server.go` の `Shutdown` の末尾がすべてを語る。

```go
for {
    if s.closeIdleConns() {
        return lnerr          // 経路A: 全接続がアイドルになった
    }
    select {
    case <-ctx.Done():
        return ctx.Err()      // 経路B: 渡した ctx が切れた
    case <-timer.C:
        timer.Reset(nextPollInterval())
    }
}
```

**経路A：`lnerr`** — listener の `Close()`（fd の `close(2)`）が失敗した場合。実用上ほぼ発生しないし、発生しても打てる手がない。

**経路B：`ctx.Err()`（= `context.DeadlineExceeded`）** — こちらが本体。意味は「**接続がまだ生きているのに待つのをやめた**」＝処理中のリクエストを途中で切り捨てた、という事実。

これは「対処するエラー」ではなく「**観測すべき事実**」。ログに残っていれば「ドレイン時間が短すぎる」または「返ってこないハンドラがいる」と分かる。残っていなければ、リクエストを落としたこと自体に永久に気づけない。

> **重要**：`Shutdown(context.Background())` と書くと deadline が無いので経路B は絶対に通らない。返るのは `lnerr` だけになり、戻り値が実質無意味になる。**タイムアウトを付けて初めて戻り値が意味を持つ。**

ログ文言を "timeout" と断定しないこと。経路A の可能性があるので `log.Printf("graceful shutdown: error %v", err)` のように書き、エラー自身に名乗らせる。

---

## 7. タイムアウトの入れ子

```
Shutdown の ctx (25s)  <  ECS StopTimeout / K8s terminationGracePeriodSeconds (30s)
```

**プラットフォームの締め切りより内側に自分の締め切りを置く。**

SIGKILL は捕捉できない。外から殺されると**ログに何も残らない**（「タスクが消えた」という事実しか手元にない）。自分で上限を持てば「N 秒待ったが接続が残っていた」を自分のログに書ける。障害の切り分けが可能かどうかの分かれ目。

値そのものより、この構図の方が重要。ECS でも Kubernetes でも同じ。

---

## 8. `WriteTimeout` の実体（誤解しやすい）

`server.go` がやっているのは1行だけ。

```go
if d := c.server.WriteTimeout; d > 0 {
    defer func() {
        c.rwc.SetWriteDeadline(time.Now().Add(d))
    }()
}
```

生の `net.Conn` に write deadline を設定しているだけ。**deadline は「時間が来たら何かが起きるタイマー」ではなく、「以降の `Write` が即エラーを返すようになる時刻」**。`Write` が呼ばれなければ何も起きないし、コネクションが自動的に閉じられることもない。

したがって：

- **ハンドラの実行時間を縛らない。** Go には goroutine を外から中断する手段がない。DB 待ちで 60 秒固まったハンドラは、`WriteTimeout` 10 秒を過ぎても走り続ける
- ハンドラが return するまでコネクションは解放されないので、`closeIdleConns()` の対象にもならず、`Shutdown` は待たされ続ける

### では何から守っているか

**書き込みが本当にブロックするケース**、つまり `Write` がカーネルの送信バッファに入りきらず待たされる場合。バッファが埋まる原因は**クライアントが読まないから**。

- レスポンスを要求しておいて極めて低速に読む（あるいは全く読まない）クライアント＝ slow read 攻撃
- 消えたクライアント（TCP レベルではまだ切断が検知されていない）

`WriteTimeout` なしだとこの接続が永久に残る。攻撃側はコストゼロで接続を大量に握れる。

### 整理

| 縛る対象 | 手段 |
|---|---|
| **クライアント起因の遅さ** | `ReadHeaderTimeout` / `ReadTimeout` / `WriteTimeout` / `IdleTimeout` |
| **サーバ起因の遅さ（ハンドラ）** | ハンドラ内で `req.Context()` を監視して自発的に return、あるいは `http.TimeoutHandler` |
| **シャットダウン全体の上限** | `Shutdown` に渡す ctx のタイムアウト |

`http.Server` のタイムアウト群はすべて前者。シャットダウン時間を実際に縛っているのは `Shutdown` の ctx **だけ**。

---

## 9. 動作確認の実測

`/superslow`（`time.Sleep(30s)` するテスト用ハンドラ）を使って検証した。設定は `WriteTimeout: 10s` / `Shutdown ctx: 25s`。

### 起動失敗 → errCh 経路

同じポートで2つ目を起動：

```
HTTP server ListenAndServe failed: listen tcp :8080: bind: address already in use
exit code 1
```

戻り値を捨てていたら、ここで黙って生き続けていた。

### 待ち切れないケース（ハンドラ 30s > 残り猶予 25s）

```
18:21:32  start some slow procedure
18:21:34  graceful shutdown: started      ← SIGTERM
18:21:59  graceful shutdown: error context deadline exceeded   ← +25s
```

- 「complete some slow procedure」は**出ていない**。ハンドラは走行中のままプロセスが終了した
- クライアント側は `curl: (52) Empty reply from server`（26.99 秒）
- シャットダウン中の新規接続は `curl: (7) Failed to connect` = **listener は即座に閉じている**

### 待ち切るケース（SIGTERM を後ろにずらして残り猶予 25s > 22s に）

```
18:22:36  start some slow procedure
18:22:44  graceful shutdown: started      ← SIGTERM
18:23:06  complete some slow procedure    ← +22s、Shutdown は nil を返し exit 0
```

ハンドラを最後まで待ってから終了できている。

### ここで得た教訓

**待ち切れたのに、クライアントは `Empty reply from server` を受け取った。**

`WriteTimeout` が 10 秒なので、18:22:46 の時点で write deadline を過ぎている。18:23:06 にハンドラが書き込もうとしても、その `Write` は即エラーになる。

> `Shutdown` がサーバにとって graceful でも、`WriteTimeout` より長いハンドラがあればクライアントには何も届かない。

タイムアウト同士は独立に設定できてしまうが、**整合していないと意味を成さない**。`WriteTimeout` は実際のハンドラの所要時間より長く取る必要がある。
