# Redis cache-aside とテーブルドリブンテスト（Day 5）
Claudeによる学習内容まとめ。  
`GET /posts/{id}` に cache-aside を入れ、更新時の無効化まで実装した際の設計判断と、repository interface の手書きモックを使ったテーブルドリブンテストを整理する。  
派生して踏んだ「`defer Close()` は必要か」「in-flight とは何か」も併せて残す。

```go
// internal/service/post.go — 今日入った中核
func (s *PostService) GetPost(ctx context.Context, id uint) (*model.Post, error) {
	cached, err := s.cache.GetPost(ctx, id)
	switch {
	case err == nil:
		log.Printf("cache HIT: post:%v", id)
		return cached, nil
	case errors.Is(err, model.ErrCacheMiss):
		log.Printf("cache MISS: post:%v", id)
	default:
		log.Printf("cache ERROR: post:%v: %v", id, err) // fail open
	}

	post, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.cache.SetPost(ctx, post); err != nil {
		log.Printf("cache SET failed: post:%v: %v", id, err)
	}
	return post, nil
}
```

---

## 0. 全体像

| 変更 | 内容 |
|---|---|
| `repository/kvs.go` | `TryRedis`（疎通確認のみ）を `InitRedis` に作り替え。`*redis.Client` を返す |
| `repository/post_cache.go` | 新規。`PostCache` が Get/Set/Delete と JSON 直列化・キー生成を担う |
| `service/post.go` | `postCache` interface を追加。`GetPost` に cache-aside、`UpdatePost`/`DeletePost` に無効化 |
| `model/post.go` | `Tags` に `json:"-"` を付与（キャッシュ対象外） |
| `model/errors.go` | `ErrCacheMiss` を追加 |
| `cmd/api-server/main.go` | Redis 初期化を配線。`Close` は DB・KVS とも**意図的に省略**（理由は §8） |
| `service/post_test.go` | 新規。`postRepository` / `postCache` の手書きモック ＋ `GetPost` のテーブルドリブンテスト4ケース |


---

## 1. cache-aside とは何を指すか

「**アプリケーションがキャッシュと DB の両方を直接見て、整合を自分で取る**」パターン。lazy loading とも呼ばれる。

```
Read:   cache を見る → MISS なら DB → 結果を cache に書き戻す
Write:  DB を更新する → cache を消す（無効化）
```

対比されるのは、キャッシュ層自身が DB を読み書きする **read-through / write-through**。こちらはアプリからは単一のデータストアに見えるが、そういう機能を持つミドルウェア（あるいはライブラリ）が要る。Redis は素の状態では read-through を提供しないので、**Redis ＋ アプリという構成なら自動的に cache-aside になる**。

cache-aside の性質：

- **キャッシュが落ちてもアプリは動く**（DB に直接行けるため）。これが後述の fail open の前提
- **最初のアクセスは必ず MISS**（事前に温めない限り）。デプロイ直後やキャッシュ再起動直後にレイテンシが跳ねるのはこのため
- **整合の責任がアプリ側にある**。無効化を書き忘れると stale がそのまま出る

配置は service 層にした。repository を `cachedPostRepository` でラップして handler/service を無変更にする案もあったが、`postRepository` は7メソッドあるので使わないメソッドまで委譲する必要が出る。「キャッシュするかどうかはユースケースの判断であって、永続化の詳細ではない」と考えると service 側で正しい。

---

## 2. キャッシュミスをどう表現するか

go-redis は「キーが存在しない」を **`redis.Nil` というエラー値**で返す。ここを通信エラーと区別しないと、Redis が落ちているのに MISS として扱ってしまう。

```go
b, err := c.client.Get(ctx, postKey(id)).Bytes()
if err != nil {
	if errors.Is(err, redis.Nil) {
		return nil, model.ErrCacheMiss   // 「無い」
	}
	return nil, fmt.Errorf("...: %v", err) // 「壊れている」
}
```

`redis.Nil` をそのまま service へ漏らさず `model.ErrCacheMiss` に変換しているのは、**service が go-redis に依存しないようにするため**。interface で repository を抽象化していても、エラー値が具象ライブラリのものだと結局そこに縛られる。

ミスの表現方法は2択あった。

| 案 | 形 | 評価 |
|---|---|---|
| **sentinel error**（採用） | `(*model.Post, error)` ＋ `ErrCacheMiss` | 既存の `ErrPostNotFound` と揃う。go-redis 自身も `redis.Nil` でこの流儀 |
| bool を返す | `(*model.Post, bool, error)` | 「無い」と「壊れた」が型で分かれて明快だが、コードベース内で流儀が混在する |

**戻り値の設計は「どちらが正しいか」より「コードベース内で揃っているか」が優先される。**

`Get(...).Bytes()` は `[]byte` で取り出すヘルパ。`.Result()` だと `string` になるので `json.Unmarshal` の前に変換が要る。

---

## 3. 何をキャッシュするか ── model か、レスポンスか

キャッシュに入っている実体：

```json
{"ID":11,"CreatedAt":"...","UpdatedAt":"...","DeletedAt":null,"Body":"...","UserID":1}
```

API が返すのは `{"id":11,"userId":1,"body":...}`（`postsOutput` DTO）。つまり **domain model をキャッシュし、handler で毎回 DTO に詰め替えている**。

| キャッシュ対象 | 利点 | 欠点 |
|---|---|---|
| **model**（採用） | 複数エンドポイント・複数の出力形式から再利用できる。API 仕様を変えてもキャッシュ無効化が不要 | DTO 変換コストは毎回かかる |
| レスポンス JSON | 変換コストも省ける。バイト列をそのまま返せて最速 | 出力形式を変えた瞬間、全キャッシュが古い形式になる |

実務では **model 側が無難**。キャッシュのライフサイクルと API 仕様のライフサイクルを分離できる。レスポンスキャッシュが勝つのは、変換コストが支配的な重い集計 API か、そもそも CDN / リバースプロキシで持つ場合。

### relation をキャッシュに含めるか

`Post.Tags` は `json:"-"` で除外した。判断基準は「**そのキャッシュ経由でデータを返す API が、その relation を扱うか**」。

含めた場合、`Post.Body` が変わらなくても `Tags` の変更で stale になるため、**Tag 側の更新でも Post のキャッシュを消す必要が出る**。無効化すべき箇所が relation の数だけ増えるので、キャッシュに入れる範囲は狭いほど安全。

なお `model.Post` に json タグを付けたことで、**キャッシュ以外の全 JSON 処理にも影響する**。今は handler が DTO を挟んでいるので実害ゼロだが、「永続化層のモデルにシリアライズの都合が混ざった」状態ではある。将来 Tags を API で返したくなった時に引っかかる。

---

## 4. fail open / fail closed

`GetPost` の `default` 節（＝ MISS でもない、本物のキャッシュ障害）をどう扱うか。

```go
default:
	log.Printf("cache ERROR: ...")
	// そのまま下へ抜けて DB を読む
```

| 方針 | 挙動 | 適する状況 |
|---|---|---|
| **fail open**（採用） | キャッシュ障害を無視して DB へフォールバック | キャッシュが純粋に高速化のための任意層。DB が全トラフィックを捌ける |
| fail closed | エラーを返してリクエストを落とす | DB がキャッシュ前提でサイジングされている場合 |

fail open の危険は、**Redis が全落ちした瞬間に全トラフィックが DB に殺到する**こと（thundering herd）。DB が耐えられなければ、キャッシュの障害がシステム全体の障害に昇格する。

実務では中間解を取ることが多い：フォールバックに流量制限（同時実行数の上限）をかけ、溢れた分だけ 503 を返す。「全部通す」か「全部落とす」かの二択ではない。

キャッシュ**書き込み**の失敗（`SetPost`）は無条件にログのみ。失敗しても次回 MISS するだけで、リクエストの結果は正しいため。

---

## 5. 無効化 ── 順序・手段・失敗時

```go
func (s *PostService) UpdatePost(ctx context.Context, id uint, body string) error {
	if body == "" {
		return model.ErrInvalidPostInput
	}
	if err := s.repo.Update(ctx, id, body); err != nil {
		return err
	}
	s.invalidatePostCache(ctx, id)  // DB 更新の「後」
	return nil
}
```

### (a) なぜ DB 更新の「後」なのか

先に消すと、更新が完了するまでの隙間に別リクエストが来た場合：

```
req A: cache DEL
req B: cache MISS → DB から【古い値】を読む → cache へ格納
req A: DB UPDATE 完了
→ cache には古い値が残る（更新後なのに stale）
```

後に消せばこの窓は塞がる。ただし**完全にゼロにはできない**（DEL 直前に MISS した B が、DEL 後に Set を完了させる競合は残る）。そこを最終的に回収するのが TTL。

### (b) なぜ「消す」であって「上書き」ではないか

`DeletePost` ではなく `SetPost` で新しい値を書く案（write-through 風）もあるが、**並行更新で順序が逆転すると恒久的に stale が残る**。

```
req A: DB UPDATE (body=X) → cache SET (X)
req B: DB UPDATE (body=Y) → cache SET (Y)
```

DB とキャッシュへの到達順が入れ替わると、DB は Y なのにキャッシュは X で固定される。DEL なら「次回読み込みで必ず DB を見に行く」ので、どんな順序でも最終的に正しくなる。**DEL が定石。**

### (c) DEL に失敗したら

```go
func (s *PostService) invalidatePostCache(ctx context.Context, id uint) {
	if err := s.cache.DeletePost(ctx, id); err != nil {
		log.Printf("cache DEL failed: post:%v: %v", id, err)
	}
}
```

リクエストは成功させる。これは「**TTL が切れるまでの stale を許容する**」という明示的な判断。許容できない要件なら、リトライか outbox パターン（更新イベントを永続化して非同期に無効化する）が要る。

### (d) TTL の本当の役割

TTL 60秒は性能のためではなく、**無効化漏れ・無効化失敗に対する最後の保険**。

`redis-cli ttl post:11` のカウントダウンは、そのまま「**事故が続く時間の上限**」を意味する。TTL を無限にすると、DEL に一度失敗したキーは永久に stale になる。

---

## 6. 無効化しないと何が起きるか（実演の記録）

無効化を実装する前に、実際に踏んだ挙動：

```
POST   /posts        → 201, id=11
GET    /posts/11     → MISS → DB → キャッシュ充填
DELETE /posts/11     → 204
GET    /posts/11     → HIT → 【削除済みの Post が 200 で全項目返る】
```

**UPDATE より DELETE の方が症状が重い。** 「古い本文が見える」は不便で済むが、「消したはずのデータが見え続ける」は退会処理や論理削除でそのまま情報漏洩の事故になる。無効化の必要性を説明するなら DELETE の例を出す方が強い。

今回の DELETE は gorm の論理削除（`deleted_at` に時刻をセット）なので DB にレコードは残るが、`FindByID` は `deleted_at IS NULL` で除外する。つまり **DB 経由なら 404、キャッシュ経由なら 200** という食い違い。物理削除でも結果は同じ。

無効化実装後：

```
DELETE /posts/11  → 204（+ cache DEL）
GET    /posts/11  → MISS → DB → record not found → 404
```

---

## 7. 未対応：cache penetration（キャッシュ貫通）

現在の実装は **404 をキャッシュしていない**。存在しない ID への GET は毎回 DB に到達する。

存在しない ID を大量に投げられると、キャッシュを素通りして DB だけが殴られる。対策は2つ。

- **negative caching**：空の結果を短い TTL（数秒〜数十秒）でキャッシュする。TTL を短くするのは、後からそのレコードが作られた時に長く 404 を返し続けないため
- **Bloom filter**：存在する ID の集合を確率的データ構造で保持し、確実に存在しないものは DB に行かせない

今日は未実装。関連する用語として、**キャッシュ雪崩（cache avalanche）**＝ 大量のキーが同時に expire して DB に殺到する現象があり、こちらは TTL にランダムなジッタを足して回避する。

---

## 8. `defer Close()` は必要か ── in-flight の話

Redis 初期化を足した際に `defer kvs.Close()` を書くべきか迷ったことから派生した論点。

### 結論：DB・KVS とも Close を省略した（両方で揃えた）

`redis.Client.Close` も `sql.DB.Close` も、godoc に「**It is rare to Close ... meant to be long-lived**」とある。**ハンドルの寿命 ＝ プロセスの寿命**というライブラリ側の設計であり、プロセス終了時に OS が fd を回収するのでリソースリークにもならない。

逆に言えば、**プロセスの寿命 > コネクションプールの寿命**になる使い方（リクエストごとやジョブごとにプールを作る、テストで複数回 `Open` する、複数テナントのプールを動的に張り替える等）では、明示的に `Close` しないと leak する。「Close 不要」はライブラリの性質ではなく**この構成に限った帰結**である、という理解が本体。

当初は「書いても害はなく、順序の型が残る」と考えて片方（Redis）にだけ入れていた。`defer` は `main` の return 時、つまり `s.Shutdown()` の後に走るため：

```
SIGTERM → s.Shutdown()（処理中リクエストの完了を待つ）→ kvs.Close()
```

という順になり、「**サーバを止めてから依存を解放する**」graceful shutdown の型が構造的に保証される、という理由だった。逆順（依存を先に閉じる）だと処理中のリクエストが `redis: client is closed` を踏む。

最終的に省略に倒したのは、**片方だけ Close する非対称に根拠がなかった**ため。「どちらでもよいが、揃っていること」が重要で、今回は省略側で揃えた。

### `in-flight` と、Close が実際にやること

**in-flight** ＝「開始済みで、まだ完了していない」処理。飛行機が離陸済みで未着陸の状態の比喩。対義的に使われるのが idle（何も処理していない）。

| 層 | in-flight の中身 | 完了を待つ仕組み |
|---|---|---|
| HTTP | レスポンスを返し終わっていないリクエスト | `s.Shutdown()`（sdCtx の25秒まで待つ） |
| DB | 発行済みで結果が返ってきていないクエリ | **無し** |
| Redis | 送信済みで応答待ちのコマンド | **無し** |

> **要注意：`sql.DB.Close()` は in-flight を待たない。**
>
> godoc にはこうある：
> > Close closes the database and prevents new queries from starting. **Close then waits for all queries that have started processing on the server to finish.**
>
> この2文目は「Close がブロックする」と読めてしまうが、**実装はブロックしない**。`database/sql/sql.go`（Go 1.26.5）の `(*DB).Close` がやっているのは、
>
> 1. `freeConn`（**idle なコネクションのみ**）を閉じる
> 2. `db.closed = true` を立てて以降の新規クエリを弾く
> 3. cleaner goroutine を止め、connector を閉じる
>
> の3つだけで、**待つコードが存在しない**。in-use なコネクションには触れず、`putConn` で返却された時点で（`db.closed` なので）破棄されるだけ。
>
> godoc の文は「**実行中のクエリを強制中断せず走り切らせる**」の意と読むのが実装と整合する。ドキュメントの文言だけで挙動を断定せず、疑わしければ `$(go env GOROOT)/src/` を読むこと。

したがって `defer sqlDB.Close()` を足しても、**`Shutdown` の25秒の予算は壊れない**。実際に起きるのは、`Shutdown` が諦めて放置した handler がその後に新しいクエリを発行しようとすると `sql: database is closed` で失敗する、という程度のこと。

`redis.Client.Close()` も同様に in-flight を待たない（autopipeliner を止めてリソースを解放するだけ）。

### では何が in-flight を止めるのか

**どちらの Close でもない。** DB のクエリを実際に打ち切れるのは ctx のキャンセルだけで、それも「Go プロセス側が待つのをやめる」だけである（サーバ側は走り続ける）。詳細は §9。

`Shutdown` の25秒を超えた分を本当に打ち切りたいなら、`Close` ではなく `BaseContext` の cancel が必要。なお待ち時間の最終的な上限は、実務では**プロセス内ではなく platform 側が握る**（ECS の `stopTimeout` / K8s の `terminationGracePeriodSeconds` を超えれば SIGKILL）。`Shutdown` の25秒はハードリミットではなく「SIGKILL される前に自力で終わってログを残す」ための予算にすぎない。

### `log.Fatal` は defer を飛ばす

`case err := <-errCh:` の `log.Fatalf` は内部で `os.Exit(1)` を呼ぶため、**`defer` は一切実行されない**。今の `main` では `defer stop()` と `defer cancel()` がスキップされる（Close を省略したので影響範囲は狭い）。異常終了パスで直後にプロセスが消えるため実害はないが、事実として知っておく。

「defer で後始末する」設計と `log.Fatal` は本質的に相性が悪い。気にするなら `main` を薄くして本体を `run() error` に移し、`main` 側で `if err := run(); err != nil { log.Fatal(err) }` とするのが定石（`run` の return で defer が全部走ってから exit する）。

---

## 9. Shutdown のタイムアウト後、handler は止まっていない

`s.Shutdown(sdCtx)` が25秒で諦めた時、**待ちきれなかったリクエストは「中断」ではなく「放置」される**。`Shutdown` は handler goroutine を止める手段を持たない。`sdCtx` が切れると `ctx.Err()` を返して即座に戻り、handler は走ったまま残る。

### 強制的に打ち切るには BaseContext

| フィールド | シグネチャ | 呼ばれる頻度 |
|---|---|---|
| `BaseContext` | `func(net.Listener) context.Context` | **リスナーごとに1回**。全リクエストの ctx の根っこを返す |
| `ConnContext` | `func(ctx context.Context, c net.Conn) context.Context` | **TCP接続ごとに1回**。第1引数で BaseContext の結果を受け取り派生させる |

`ConnContext` は接続単位の値を ctx に載せるためのフック。全体を一斉に cancel したいなら `BaseContext` に自前の cancelable ctx を返す。

```go
baseCtx, cancelBase := context.WithCancel(context.Background())
defer cancelBase()

s := &http.Server{
	BaseContext: func(net.Listener) context.Context { return baseCtx },
	// ...
}

// ...
if err := s.Shutdown(sdCtx); err != nil {
	log.Printf("Graceful shutdown: error %v", err)
	cancelBase()  // drain を待ちきれなかった分だけ強制的に打ち切る
}
```

**cancel は Shutdown の後**。先に呼ぶと、正常に終われたはずのリクエストまで殺す。

### cancel すると何が止まり、何が止まらないか

**Go プロセス側**：`go-sql-driver/mysql` は `watchCancel` で `ctx.Done()` を監視する goroutine を立てている。cancel されるとコネクションを bad とマークし、下の `net.Conn`（TCPソケット）を閉じる。`database/sql` は bad なコネクションをプールに戻さず破棄する。**クエリ待ちをしていた goroutine は即座にエラーで返る**ので、Go 側から見れば処理はここで終わる。

**MySQL サーバ側**：**止まらない。** MySQL はクエリ実行中にクライアントの生死をポーリングしない。気づくのは**結果を書き戻そうとした瞬間**で、ソケットが閉じているので書き込みが失敗し（broken pipe ＝ 相手が閉じた通信路への書き込みエラー）、そこで初めてスレッドが終了する。

つまり `SELECT SLEEP(60)` は**60秒きっちりサーバ側で走り切ってから**終わる。タイムラグは「多少」ではなく**残りのクエリ実行時間まるごと**。行を逐次返す大きな SELECT なら途中の書き込みで早期に気づくが、集計やソートで出力が出ない間は気づけない。

> これが「**アプリは1秒でタイムアウトして 503 を返しているのに、DB の CPU は張り付いたまま**」という障害の正体。しかもクライアントはリトライするので、サーバ上の実行中クエリだけが積み上がる。

**DB による違い**：PostgreSQL はプロトコルに CancelRequest があり、`lib/pq` / `pgx` は ctx cancel 時に**別コネクションを張ってキャンセル要求を送る**ので、サーバ側も速やかに止まる。MySQL プロトコルにはこれが無く、ドライバはコネクションを閉じるだけ。MySQL でサーバ側を確実に止めたいなら、別コネクションから `KILL QUERY <thread_id>` を撃つか、サーバ変数 `max_execution_time`（SELECT 限定・ミリ秒）を使う。

### タイムアウト制御の4階層（優先順位）

shutdown 時の cancel は**バックストップ**であって第一防衛線ではない。

1. **リクエスト／クエリ単位の deadline**（最優先）— handler で `context.WithTimeout(r.Context(), N)` を切って repository まで流す。そもそも60秒走る handler を存在させない
2. **DB サーバ側の保険** — MySQL なら `max_execution_time`。アプリのバグや ctx 伝播漏れをサーバ側で止める
3. **shutdown 時の `BaseContext` cancel** — 1・2 をすり抜けた分の後始末
4. **platform の grace period** — 最後に SIGKILL

補足：`http.Server.WriteTimeout` は handler を止めない。`http.TimeoutHandler` も**レスポンスを差し替えるだけで handler goroutine は走り続ける**。Go で handler の実行を本当に止められる手段は ctx の伝播のみで、しかも「止まる」のは ctx を見ているライブラリ（`database/sql`、`net/http` のクライアント等）に到達した時だけ。**純粋な CPU ループは cancel しても止まらない。**

---

## 10. 手書きモック ── interface の大きさが痛みとして出る

```go
type mockPostRepository struct {
	findByIDFunc  func(ctx context.Context, id uint) (*model.Post, error)
	findByIDCalls int
}

var _ postRepository = (*mockPostRepository)(nil)  // コンパイル時の充足検査

func (m *mockPostRepository) FindByID(ctx context.Context, id uint) (*model.Post, error) {
	m.findByIDCalls++
	return m.findByIDFunc(ctx, id)
}

// 以下、使わないがinterfaceを満たすために必要
func (m *mockPostRepository) FindAll(ctx context.Context) ([]model.Post, error) {
	panic("FindAll: not implemented")
}
// ... あと5本
```

**モックの設計上のポイント**：

- 振る舞いを**関数フィールド**で持たせると、テストケースごとに差し替えられる。これがテーブルドリブンの表に「モックの挙動」を書けるようにする鍵
- **呼び出し回数をカウント**すると、「repository に到達しなかったこと」＝ キャッシュが効いたことを検証できる。戻り値だけ見ていては分からない
- 使わないメソッドは `panic` にする。誤って呼ばれたら即座に落ちて気付ける（ゼロ値を返すと静かに間違ったテストが通る）

**体感された事実**：`postRepository` は7メソッドあるので、`FindByID` 1本を試したいだけで6個の捨てメソッドを書く羽目になる。これが「**interface は小さく保て**」の実物。Go の慣習で interface を利用側（service）に置くのは、まさに「使う分だけ宣言する」ためだが、今回は CRUD 全部を1つの interface に入れているのでその利点が出ていない。

実務では、この痛みが閾値を超えた時点で **interface を分割する**か、**モック生成ツール（`mockgen` 等）を入れる**かの判断をする。今回は痛みを覚えることが目的なので分割していない。

---

## 11. テーブルドリブンテストの型

```go
tests := []struct {
	name          string
	cacheGet      func(ctx context.Context, id uint) (*model.Post, error)  // モックの振る舞い
	repoFind      func(ctx context.Context, id uint) (*model.Post, error)
	wantBody      string
	wantErr       error  // errors.Is で比較
	wantRepoCalls int
	wantSetCalls  int
}{ /* ... */ }

for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) {
		repo := &mockPostRepository{findByIDFunc: tt.repoFind}
		cache := &mockPostCache{getPostFunc: tt.cacheGet}
		svc := NewPostService(repo, cache)

		got, err := svc.GetPost(context.Background(), 1)

		if !errors.Is(err, tt.wantErr) {
			t.Fatalf("GetPost() error = %v, want %v", err, tt.wantErr)
		}
		// ...
	})
}
```

| 要素 | 役割 |
|---|---|
| 匿名構造体のスライス | ケース一覧。慣例的に変数名は `tests`、要素は `tt` |
| `name` | ケース名。**必須**。失敗時にどれが落ちたか分かる |
| `want` 系 | 期待値。エラーは `wantErr` として別立てにする |
| `t.Run(name, func)` | **サブテスト**。ケース名が出力に出る／1ケース失敗しても他が走る／`-run 'TestX/ケース名'` で絞れる／`t.Parallel()` を呼べる |

**本質はケース追加が構造体リテラル1個で済むこと。** 検証ロジックが1箇所なので、検証条件を変えたい時も1箇所で済む。

### 今回のテストで検証したこと

| ケース | 検証内容 |
|---|---|
| 1. cache HIT | `wantRepoCalls: 0` — **repository に到達しないこと**＝キャッシュが効いている |
| 2. cache MISS | `wantSetCalls: 1` — DB の値をキャッシュに書き戻していること |
| 3. repository エラー | `errors.Is(err, model.ErrPostNotFound)` — **エラーを握りつぶさず素通ししていること** |
| 4. cache 障害 | `wantErr: nil` かつ `wantRepoCalls: 1` — **fail open の設計意図をテストで固定** |

ケース3で `fmt.Errorf("...: %w", model.ErrPostNotFound)` と `%w` で包んでいるのは、実際の repository がそうしているため。`errors.Is` は wrap を辿って比較するので、包まれていても検出できる。

### 慣例と落とし穴

- 変数名は **`got`** と **`want`**。メッセージは `"XXX() = %v, want %v"`
- **`t.Error` と `t.Fatal` の使い分け** — `Fatal` はそのサブテストを即中断。「`got` が nil のまま先に進むと panic する」場面で使い、それ以外は `Error` にして検証を続ける（1回の実行で失敗箇所をまとめて把握できる）
- **`tt := tt` は不要**（Go 1.22 以降）。以前はループ変数が全イテレーションで共有されていて、`t.Parallel()` と組み合わせると全ケースが最後の値を見るバグがあったためこの行が必須だった。古い記事には必ず入っているが、真似しなくてよい
- **ケースごとに手順そのものが違う場合は表にしない**。フィールドが `if tt.shouldSetup { ... }` のようなフラグだらけになったら、別のテスト関数に分けるサイン

### テストの強度をどこまで上げるか

今回のモックは `SetPost` の**回数**は数えるが**引数**は見ていない。別の Post をキャッシュに書くバグがあっても通る。引数まで固定すると強くなるが、**実装変更のたびにテストが壊れる（脆いテスト）**というコストがある。常に引数まで見ればいいわけではない、という判断が要る箇所。

一次資料：[go.dev/wiki/TableDrivenTests](https://go.dev/wiki/TableDrivenTests)

---

## 12. 用語メモ

| 用語 | 意味 |
|---|---|
| **cache-aside / lazy loading** | アプリがキャッシュと DB を両方見て整合を取るパターン |
| **stale cache** | 元データが更新されたのに古いままのキャッシュ |
| **in-flight** | 開始済みで未完了の処理（対：idle） |
| **fail open / fail closed** | 依存が壊れた時に通すか落とすか |
| **thundering herd** | キャッシュ全落ち等で大量リクエストが一斉に DB へ殺到する現象 |
| **cache penetration（貫通）** | 存在しないキーへのアクセスがキャッシュを素通りして DB に届く |
| **cache avalanche（雪崩）** | 大量のキーが同時に expire して DB に殺到する（TTL にジッタを足して回避） |
| **negative caching** | 「存在しない」という結果を短い TTL でキャッシュする |
| **broken pipe** | 相手が閉じた通信路への書き込みで返るエラー |
| **outbox パターン** | 更新イベントを DB に永続化し、非同期に外部（キャッシュ・MQ）へ反映する |

---

## 13. 持ち帰り（未着手）

- **negative caching / cache penetration 対策** — 404 をキャッシュしていない
- **一覧（`ListPost`）のキャッシュ** — 単体と違い「どのキーを消せばいいか分からない」問題が出る。キーのバージョニングやタグ付けで解く
- **`BaseContext` による shutdown 時の強制 cancel** — 概念のみ整理。実装は未着手
- **`main` の `run() error` 化** — `log.Fatal` が defer を飛ばす件への対処。今は Close を省略しているため優先度は低い
- **interface の分割 or モック生成ツール** — 7メソッドの捨て実装を書く痛みは体感済み
