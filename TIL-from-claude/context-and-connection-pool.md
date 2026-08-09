# context 伝播とコネクションプール枯渇（Day 4）
Claudeによる学習内容まとめ。`r.Context()` を DB まで通し、`SetMaxOpenConns` を絞ってプール枯渇を実測した際の観察と、そこから導かれる運用上の判断を整理する。

```go
// internal/handler/post.go — 今日入った中核
ctx, cancel := context.WithTimeout(r.Context(), time.Duration(to)*time.Second)
defer cancel()

if err := h.svc.Sleep(ctx, sleep); err != nil { ... }
```
```go
// internal/repository/post.go — 終端。ここで初めて ctx が DB ドライバに渡る
func (r *PostRepo) Sleep(ctx context.Context, sec int) error {
	return r.db.WithContext(ctx).Exec("SELECT SLEEP(?)", sec).Error
}
```

---

## 0. 全体像

| 変更 | 内容 |
|---|---|
| `repository` | 全 CRUD メソッドの第1引数に `ctx context.Context`。gorm 呼び出しを `WithContext(ctx)` に |
| `service` | `postRepository` interface と `PostService` の全メソッドに `ctx` を追加（素通し） |
| `handler` | `r.Context()` を service へ渡す |
| 新規 | `GET /slowquery`（`SELECT SLEEP(n)` + 可変 timeout）、`GET /dbstats`（`sql.DBStats`） |
| 新規 | `cmd/loadgen`（`sync.WaitGroup` による N 並列クライアント） |
| 設定 | `db.DB()` で `*sql.DB` を取得し `SetMaxOpenConns` |

ctx は**引数として明示的に通す**のが Go の規約。struct のフィールドに保持しない（リクエストごとに異なる値なのに、`PostRepo` はプロセス起動時に1つ作られて全リクエストで共有されるため）。

---

## 1. `WithContext` は何をしているか

```go
// 誤解しやすい形：r.db 自体は書き換わらない
r.db.WithContext(ctx).Find(&posts)
```

gorm のチェーンメソッドは**新しい `*gorm.DB` を返す**。`r.db` は共有されるハンドルなので、毎回チェーンの先頭に付ける必要がある。

最終的に ctx がどこへ行くかというと、`database/sql` の `QueryContext` / `ExecContext` の第1引数。**標準ライブラリの `database/sql` が最初から ctx 対応している**ので、gorm はそれを橋渡ししているだけ。

複数クエリを発行するメソッドは**全部に付ける**。

```go
func (r *PostRepo) Update(ctx context.Context, id uint, body string) error {
	if err := r.db.WithContext(ctx).First(&post, id).Error; err != nil { ... }   // 1本目
	if err := r.db.WithContext(ctx).Model(&post).Updates(...).Error; err != nil { ... }  // 2本目
}
```

片方だけだと、SELECT はキャンセルされるが UPDATE は止まらない、という中途半端な状態になる。

---

## 2. `r.Context()` はいつ Done になるか ── 「timeout」は入っていない

公式ドキュメント（`net/http`）が挙げるのは3つ：

| 条件 | 説明 |
|---|---|
| クライアント接続のクローズ | ブラウザを閉じた、curl を Ctrl-C した、など |
| リクエストのキャンセル（HTTP/2） | `RST_STREAM` フレームによる通知。RST = reset。HTTP/2 は1本の TCP 接続に複数リクエスト（ストリーム）を多重化するので、接続を切らずに特定ストリームだけ中止できる |
| `ServeHTTP` の return | handler が終わればその ctx は用済み |

### ここが今日の一番の落とし穴

**`r.Context()` にはデフォルトで deadline が無い。** `ctx.Deadline()` は `ok == false` を返す。

```go
// cmd/api-server/main.go
s := &http.Server{
	ReadTimeout:  5 * time.Second,
	WriteTimeout: 10 * time.Second,   // ← これは ctx をキャンセルしない
}
```

`Server.ReadTimeout` / `WriteTimeout` が設定するのは **`net.Conn` の I/O デッドライン**であって、リクエスト context ではない。`WriteTimeout` を超過しても handler は動き続け、レスポンスを書こうとした時に初めてエラーになる。

| やりたいこと | 手段 |
|---|---|
| 接続レベルの保護（Slowloris 等） | `Server.ReadHeaderTimeout` / `ReadTimeout` / `WriteTimeout` |
| handler 処理そのものに時間制限 | `context.WithTimeout(r.Context(), ...)` を自分で書く |
| 全 handler に一律の時間制限 | `http.TimeoutHandler` でラップ（ctx に deadline が付き、超過時 503） |

今日タイムアウトが効いたのは、`SlowQuery` の中で**自分で `WithTimeout` を書いたから**。「Server の timeout 設定があるから ctx も切れるはず」は誤り。

---

## 3. キャンセルは本当に DB まで届く（実測）

`WithTimeout(r.Context(), 2s)` + `SELECT SLEEP(n)`：

```
sleep=1 → SlowQuery finished in 1.020463958s              （成功）
sleep=2 → Sleep failed after 2.001403958s: context deadline exceeded
sleep=5 → Sleep failed after 2.001550667s: context deadline exceeded
sleep=8 → Sleep failed after 2.001206584s: context deadline exceeded
```

**sleep=5 でも sleep=8 でも 2.001 秒で返っている。** ctx が届いていなければ DB の SLEEP 完了まで待たされるはずなので、これがキャンセル伝播の証拠。

`sleep=2` / `timeout=2` も失敗した。同着に見えるが、ネットワーク往復と Go 側のオーバーヘッド分だけ deadline が先に来る。**境界値は運任せになる**という実例。

---

## 4. 返ってくるエラーの正体

```
type=context.deadlineExceededError  value=context deadline exceeded
```

`context.deadlineExceededError` は非公開型で、その唯一の値が sentinel error の `context.DeadlineExceeded`。**型で判定するのではなく `errors.Is` で判定する。**

```go
if errors.Is(err, context.DeadlineExceeded) { ... }
if errors.Is(err, context.Canceled)         { ... }
```

### 2つを混ぜてはいけない理由（運用の話）

| | 意味 | 扱い |
|---|---|---|
| `DeadlineExceeded` | **自分が遅い**。時間内に終えられなかった | 504 相当。エラーとして計上し、SLO / エラーバジェットを消費させる。アラート対象 |
| `Canceled` | 多くの場合**クライアントが去った** | 自分の障害ではない。5xx として計上**しない**。ログは info / warn |

`Canceled` を一律 500 として扱うと、ユーザーがブラウザを閉じただけでエラー率が跳ね上がり、**メトリクスが汚れて本物の障害が埋もれる**。そもそも接続が切れているので、500 を書き込んでも届かない。

ただし無視するのも誤り。`Canceled` の急増は「クライアント側 timeout が自分のレイテンシより短い」というシグナルなので、**別カウンタとして観測する**のが正解。

---

## 5. キャンセルされたコネクションは破棄される（ソース + 実測）

`go-sql-driver/mysql` v1.10.0 のソース：

```go
// connection.go:745 — コネクションごとに watcher goroutine が回っている
func (mc *mysqlConn) startWatcher() {
	go func() {
		for {
			select { case ctx = <-watcher: ... }
			select {
			case <-ctx.Done():
				mc.cancel(ctx.Err())   // ← ここ
			case <-finished:
			}
		}
	}()
}

// connection.go:573
func (mc *mysqlConn) cancel(err error) {
	mc.canceled.Set(err)
	mc.cleanup()          // → conn.Close() で TCP 接続を実際に閉じる（:178）
}

// connection.go:811 — database/sql はこれを見てプールに戻すか捨てるか決める
func (mc *mysqlConn) IsValid() bool { return !mc.closed.Load() && !mc.buf.busy() }
var _ driver.Validator = &mysqlConn{}
```

`KILL QUERY` を送るのではなく、**接続ごと閉じる**。MySQL 側は接続が切れたことを検知してスレッドを終了させる。

### 実測による確認

競合を排除するため、`MaxOpenConns=2` のままサーバを再起動し、**待ち手のいない状態で1本だけ**タイムアウトさせた：

```
（実行前）OpenConnections: 1, Idle: 1, WaitCount: 0
$ loadgen -n 1 -url ".../slowquery?sleep=2&timeout=1"
[0] status=500 elapsed=1.007071125s
（実行後）OpenConnections: 0, Idle: 0, WaitCount: 0
```

**`1 → 0`。破棄されている。**

### なぜこれが怖いか

タイムアウトは**コネクションを1本焼く**。すると次のリクエストは TCP 接続と MySQL のハンドシェイク／認証をやり直す（TLS ありなら更に重い）。

つまり「遅い → タイムアウト → コネクション破棄 → 再接続コストで更に遅い」という**自己増幅ループ**が成立する。障害が「じわじわ悪化」ではなく「崖から落ちる」形になる原因のひとつ。

> `Stats()` の `MaxIdleClosed` などのカウンタはこの破棄を数えない。**「キャンセルで何本焼けたか」は `Stats` からは追えない。**

---

## 6. コネクションプールの設定値

`db.DB()` で gorm のハンドルから `*sql.DB` を取り出して設定する（`*sql.DB` は接続1本ではなく**プールのハンドル**。スレッドセーフで、`Open` した時点では1本も接続していない lazy connection）。

| 設定 | 何を制限するか | デフォルト |
|---|---|---|
| `SetMaxOpenConns` | 同時に open できる総数（in-use + idle） | 無制限 |
| `SetMaxIdleConns` | idle として**保持**できる数。溢れた分は返却時に close | 2 |
| `SetConnMaxLifetime` | open されてからの**寿命**（in-use / idle 問わず累計） | 無期限 |
| `SetConnMaxIdleTime` | **idle のまま**放置できる時間（Go 1.15+） | 無期限 |

### 「なぜ必要か」で覚える

- **`ConnMaxLifetime`**：DB 側の `wait_timeout`、LB や NAT のアイドル切断、フェイルオーバー後に残る古い接続、DNS 変更の反映。**アプリが知らないうちに死んでいる接続を掴み続けるのを防ぐ**。無期限だと「久しぶりのアクセスで謎の `invalid connection`」が起きる。なお期限切れ判定はプールからの取り出し／返却時と cleaner の定期実行で行われるので、**実行中のクエリが寿命で切られることはない**
- **`MaxIdleConns`**：既定の 2 のままだと、バーストのたびに接続を作っては捨てる（§7 の `MaxIdleClosed: 4` がそれ）。**`MaxOpenConns` と同値にするのが定石**

### `MaxOpenConns` は大きいほど良い、ではない

制約はアプリのメモリでも DB の接続上限でもなく、**DB が同時に捌ける処理数そのもの**。同時実行を増やしすぎると DB 側でロック競合・ディスク I/O 待ち・コンテキストスイッチが増え、**スループットが逆に落ちる**（thrashing）。

つまりプールは「並列度を上げる装置」であると同時に、**「DB を守る隔壁（bulkhead）」**でもある。小さいプールは待ち行列を作るが DB は健全なまま保たれ、待ちきれないリクエストだけが速やかに失敗する。「全員が少しずつ遅くなって全滅」より「一部を切り捨てて残りを守る」方が復旧しやすい。

サイジングの出発点：`DB の max_connections ÷ 想定インスタンス数` から逆算して余裕を残す。**プールはプロセス単位**なので、コンテナをスケールアウトすると `MaxOpenConns × インスタンス数` が DB に押し寄せる。超過分は待機ではなく**拒否**される（MySQL なら `Too many connections`）。

---

## 7. 枯渇実験

`MaxOpenConns=2`、`loadgen -n 6`、`SELECT SLEEP(2)`。

### 実験A：timeout=60（十分長い）── 待ち行列の観察

```
[3] status=200 elapsed=2.029778959s
[1] status=200 elapsed=2.029051209s
[0] status=200 elapsed=4.036328125s
[5] status=200 elapsed=4.036882125s
[4] status=200 elapsed=6.040143083s
[2] status=200 elapsed=6.042152375s
```
```json
{ "OpenConnections": 2, "InUse": 0, "Idle": 2,
  "WaitCount": 4, "WaitDuration": 12100877459 }
```

2本ずつ3バッチ、**2秒刻みの階段**。6本捌くのに6秒かかっており、並列度がプールサイズで頭打ちになっている。

`WaitDuration` は**ナノ秒**で、待った goroutine の**累計**（実時間ではない）。12.10秒 = `2+2`（2バッチ目）`+4+4`（3バッチ目）。**計算と一致する。**

### 実験B：timeout=3 ── 枯渇とタイムアウトの衝突

```
[1] status=200 elapsed=2.021109625s
[4] status=200 elapsed=2.021128292s
[5] status=500 elapsed=3.008606834s
[3] status=500 elapsed=3.008742291s
[2] status=500 elapsed=3.008714458s
[0] status=500 elapsed=3.008732917s
```
```json
{ "OpenConnections": 2, "Idle": 2,
  "WaitCount": 8, "WaitDuration": 22132237459 }   // 累積値
```

**失敗した4本は、同じエラーだが原因が2種類に分かれている。**

```
t=0   A,B → conn取得、SLEEP(2)開始 / C,D,E,F → 待ち行列へ
t=2   A,B 成功・返却 → C,D が conn取得、SLEEP(2)開始 / E,F はまだ待ち
t=3   C,D,E,F 全員 deadline
        C,D : クエリ実行中に切られた（1秒経過時点）
        E,F : コネクション待ちのまま切られた ← DB に一度も到達していない
```

これは推測ではなく `WaitDuration` の差分で裏が取れる：

| | 実験A後 | 実験B後 | 差分 |
|---|---|---|---|
| `WaitCount` | 4 | 8 | **+4**（4本全員が待った） |
| `WaitDuration` | 12.1009s | 22.1322s | **+10.0314s** |

内訳は `2+2`（C,D は t=2 に取得）`+3+3`（E,F は t=3 に断念）= **10秒**。実測と一致する。

**待ち行列も ctx を尊重する。** プールが空くのを待っている間に deadline が来れば、DB に触れずに `context deadline exceeded` が返る。

> 終了後が `Idle: 2` なのは、C,D の接続が破棄された（§5）直後に、まだ待機中だった E,F のために `database/sql` が**代替接続を開いた**ため。E,F は既に諦めていたので、その新品2本がそのままアイドルとして残った。**「破棄されなかった」のではなく「破棄された後に張り直された」**。`Stats` の現在値だけ見ても区別がつかない。

### 対照実験：`MaxOpenConns=20` に変えるだけ

```
[3] status=200 elapsed=2.019546916s      （6本すべて成功、約2秒）
...
```
```json
{ "OpenConnections": 2, "Idle": 2,
  "WaitCount": 0, "WaitDuration": 0, "MaxIdleClosed": 4 }
```

**アプリのコードもクエリも timeout 値も完全に同一。** 違うのは `SetMaxOpenConns` の数字ひとつだけで、結果が「4本が 500」から「全部2秒で成功」に変わった。

`MaxIdleClosed: 4` は「6本同時に開いたが、`MaxIdleConns` の既定値 2 を超えた4本は返却時に閉じられた」という意味。§6 で「`MaxIdleConns` は `MaxOpenConns` と揃える」と書いた根拠がこれ。

---

## 8. gorm の SLOW SQL ログは「SQL が遅い」を意味しない

実験Aのサーバログ：

```
SLOW SQL >= 200ms
[6033.150ms] [rows:0] SELECT SLEEP(2)
```

MySQL は 2 秒しか働いていないのに 6033ms と記録されている。gorm は**クエリ発行の直前から計測を始める**が、その内側で `database/sql` が**コネクションの空きを待つ**。この 6033ms は `待ち4秒 + 実行2秒` の合算。

実験Bでは更に極端なことが起きている：

```
context deadline exceeded
[3000.817ms] [rows:0] SELECT SLEEP(2)
```

これは E,F（コネクションを一度も取得できなかった側）のログ。**クエリを1バイトも送っていないのに「SELECT SLEEP(2) が3秒」と記録される。**

| 観測手段 | 見えるもの |
|---|---|
| gorm の SLOW SQL ログ | 「このクエリが遅い」としか読めない |
| エラー文字列 | `context deadline exceeded`（単発タイムアウトと**完全に同一**） |
| `DBStats.WaitCount` / `WaitDuration` | **プール待ちが起きたかどうか** |

**エラーとログだけ見ていると「クエリが重い」と誤診し、EXPLAIN してもインデックスを足しても何も改善しない。** 切り分けには `WaitCount` / `WaitDuration` のメトリクス化が要る。今日の実験で得た一番実務寄りの教訓。

---

## 9. `sync.WaitGroup`

実体は**整数カウンタ1個と、それが0になるまでブロックする仕組み**だけ。goroutine の存在すら知らない。

| メソッド | やること |
|---|---|
| `Add(delta int)` | カウンタに加算（負数可） |
| `Done()` | `Add(-1)` の別名 |
| `Wait()` | カウンタが0になるまでブロック。0なら即 return |

```go
var wg sync.WaitGroup
for i := range *n {
	wg.Add(1)              // ① goroutine の外
	go func() {
		defer wg.Done()    // ②
		...
	}()
}
wg.Wait()
```

**① `Add` を外で呼ぶ理由**：`go` 文は**起動を予約するだけで即座に次行へ進む**。中で `Add` すると、実際に加算される前に `Wait()` に到達しうる。その瞬間カウンタは0なので `Wait()` は何も待たずに返り、main 終了でプログラムごと死ぬ。

**② `Done` を `defer` にする理由**：早期 return でも panic でも必ず実行される。末尾に直書きすると、エラー時の `return` でカウンタが減らず `Wait()` が永久ブロック（`all goroutines are asleep - deadlock!`）。

**③ コピーしない**：値渡しするとコピー先の `Done` はオリジナルに効かない。渡すなら `*sync.WaitGroup`。`go vet` の `copylocks` が検出する。

カウンタが**負になると panic**（`Done` の呼びすぎ）。`Add` の総和 == `Done` の回数、を常に成り立たせる。

### 古い記事の「ループ変数の罠」は Go 1.22 で解消済み

Go 1.21 以前はループ変数が全イテレーションで共有だったため `go func(i int){...}(i)` と引数で渡す定石があった。**Go 1.22 以降はイテレーションごとに新しい変数が作られる**ので、そのままキャプチャして安全。古い記事にこの定石が大量に残っているが、真似する必要はない。

### Go 1.25 の `wg.Go`

```go
wg.Go(func() { ... })   // Add(1) + go + defer Done() を1行で
```
①②の間違いが構造的に起きなくなる。今日は学習目的で明示形のまま書いた。

---

## 10. WaitGroup では足りなくなる要件

WaitGroup が答えられるのは**「全部終わったか」だけ**。要件がそれを超えると別の道具が要る。

| 要件 | 道具 | 要点 |
|---|---|---|
| 全部終わるまで待つ | `sync.WaitGroup` | |
| 結果を集める（件数既知） | WaitGroup + `results := make([]T, n)` に**インデックス固定**で書き込み | goroutine ごとに書く場所が違うので排他不要。`append` は複数ステップなので**データ競合**になる |
| 結果を集める（ストリーム的） | バッファ付き channel + `close` | バッファ無しだと受信側が `Wait()` の後ろにいる場合デッドライン。`close` は送信側が全員終わってから1回だけ |
| カウンタを共有 | `sync.Mutex` / `sync/atomic` | `count++` も「読む→足す→書く」の3ステップ |
| エラーを回収 | `errgroup` | `g.Wait()` が最初のエラーを返す |
| 1つ失敗したら全部止める | `errgroup.WithContext` | |
| 同時実行数を制限 | バッファ付き channel（セマフォ）/ `errgroup.SetLimit(n)` | WaitGroup に上限機能は無い |
| 待ちにタイムアウト | `wg.Wait()` を goroutine で包んで `select` | `Wait()` は引数も戻り値も無く**途中で抜けられない** |

### 並行処理を書いたら必ず通すコマンド

```bash
go run -race ./cmd/loadgen -n 10 -url http://localhost:8080/healthz
```

データ競合は**たいてい動いてしまう**。10並列では再現せず本番負荷で初めて壊れる類のバグなので、検出器に任せる。

### `errgroup` の要点

```go
g, ctx := errgroup.WithContext(context.Background())
g.Go(func() error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)   // ctx キャンセルで即中断
	if err != nil { return err }              // ← これが引き金で ctx がキャンセルされる
	defer resp.Body.Close()
	return nil
})
if err := g.Wait(); err != nil { ... }
```

肝は **errgroup が goroutine を強制終了するわけではない**こと（Go に外から goroutine を殺す手段は無い）。ctx のキャンセルを**各自が検知して自分で降りる**という協調による中断で、これは §3 で `SlowQuery` が DB クエリを中断したのと**同じ仕組み**。

`golang.org/x/sync` は Go チーム管理の準標準ライブラリで、標準ライブラリではない（`go get` が要る）。channel と `context.WithCancel` で手書きもできるが、20行程度の定型になる。

---

## 11. 意図的にやっていないこと（スコープ外の記録）

| 項目 | 現状 | 影響 |
|---|---|---|
| timeout 値の外部化 | `?timeout=` のクエリパラメータと handler 内のデフォルト値 | 実験用。実務では設定 or ミドルウェアで一元管理 |
| `http.TimeoutHandler` | 未導入 | 各 handler が自前で `WithTimeout` を書く必要がある |
| `Canceled` と `DeadlineExceeded` の出し分け | どちらも 500 | §4 のとおり本来は分けるべき。メトリクス化とセットで扱う領域 |
| `MaxIdleConns` / `ConnMaxLifetime` の設定 | 未設定（既定値のまま） | §6 の定石は未適用。`MaxIdleClosed` が増える状態 |
| `DBStats` のメトリクス化 | `/dbstats` を手で叩くだけ | 本来は Prometheus / Datadog に継続的に出す |
| `/slowquery`・`/dbstats` の公開 | 誰でも叩ける | 実験用エンドポイント。実務なら認証か internal ポートに隔離 |
| loadgen の HTTP クライアント | `http.Get`（**タイムアウト無し**） | 今回はサーバ側で切れるので問題にならないが、実務では `&http.Client{Timeout: ...}` 必須 |
| トランザクション内での ctx | 未確認 | `db.Transaction` と ctx の組み合わせは未検証 |

---

## まとめ：今日入った「型」

1. **ctx は第1引数で明示的に通す**。struct フィールドに持たせない。複数クエリを打つメソッドは全部に `WithContext`
2. **`r.Context()` に deadline は無い**。`Server.ReadTimeout` / `WriteTimeout` は ctx をキャンセルしない。時間制限は自分で `WithTimeout` するか `http.TimeoutHandler`
3. **キャンセルは DB ドライバまで届く**。判定は型ではなく `errors.Is(err, context.DeadlineExceeded)`
4. **`DeadlineExceeded`（自分が遅い）と `Canceled`（相手が去った）は別扱い**。後者を 5xx に混ぜるとメトリクスが汚れる
5. **キャンセルされた接続は破棄される**。タイムアウト多発は再接続コストを呼び、自己増幅ループになる
6. **プールは並列度を上げる装置であり、DB を守る隔壁でもある**。大きいほど良いわけではない。`MaxIdleConns` は `MaxOpenConns` と揃える
7. **エラー文字列と SQL ログだけでは枯渇を診断できない**。`WaitCount` / `WaitDuration` が唯一の手掛かり
8. **WaitGroup が答えるのは「全部終わったか」だけ**。結果回収・打ち切り・同時実行制限・待ちのタイムアウトはすべて別の道具
