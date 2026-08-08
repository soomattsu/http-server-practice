# レイヤードアーキテクチャへのリファクタ（handler / service / repository / model）
Claudeによる学習内容まとめ。Day 3 で flat 構成から3層に切り直した際の設計判断を、Go の慣習と対応させて整理する。

```go
// cmd/api-server/main.go — 具象型を知っている唯一の場所
func main() {
	cfg := repository.LoadCfg()
	db, err := repository.InitMySQL(cfg)
	// ...
	postRepo := repository.NewPostRepo(db)      // *repository.PostRepo
	postService := service.NewPostService(postRepo) // interface に代入される瞬間
	postHandler := handler.NewPostHandler(postService)
	router := handler.NewRouter(postHandler)
}
```

---

## 0. 全体像

| パッケージ | 責務 | import してよい内部パッケージ |
|---|---|---|
| `cmd/api-server` | 配線（どの実装を使うか決める） | 全部 |
| `internal/handler` | HTTP ⇄ アプリの変換 | service, model |
| `internal/service` | 業務ロジック | model |
| `internal/repository` | 永続化（gorm の隠蔽） | model |
| `internal/model` | ドメイン型・sentinel error | なし |

機械的な確認：

```
$ go list -f '{{join .Imports "\n"}}' ./internal/service
github.com/soomattsu/http-server-practice/internal/model    ← model だけ
```

`grep` より `go list` が確実。コメントや文字列を拾わず、実際にコンパイラが解決した import だけが出る。

---

## 1. 「一方向の依存」を守るのは規約ではなくコンパイラ

Go は**パッケージ間の循環 import をコンパイルエラーにする**。これが層分離の土台になっている。

層をパッケージとして切った時点で、`repository` が `service` を import した瞬間にビルドが壊れる。つまり依存方向はレビューで守るものではなく、**壊すとビルドが通らないもの**になる。

> SESSION.md の「循環 import が発生する＝依存方向の逆流が起きているサイン」はこれ。循環 import はバグではなく、設計の誤りをコンパイラが検出してくれている状態。

逆に言えば、層を**同一パッケージ内のファイル分割**で表現すると（`handler.go` / `service.go` を同じ package に置くなど）、この強制力はゼロになる。ファイルではなくパッケージで切る意味はここにある。

---

## 2. interface は「使う側」に置く（Go で最も重要な流儀）

```go
// internal/service/post.go
package service

// postRepository は Post用のserviceがrepositoryを"利用"する時に必要なメソッドを宣言したinterface。
type postRepository interface {
	FindAll() ([]model.Post, error)
	FindByID(id uint) (*model.Post, error)
	Create(post model.Post) (uint, error)
	Update(id uint, body string) error
	Delete(id uint) error
}

type PostService struct {
	repo postRepository
}
```

```go
// internal/repository/post.go — interface の存在を一切知らない
package repository

type PostRepo struct{ db *gorm.DB }

func (r *PostRepo) FindByID(id uint) (*model.Post, error) { /* ... */ }
```

Java / C# 流の「提供側が interface を定義し、実装が `implements` する」とは**向きが逆**。Go では利用側（service）が interface を定義し、実装側（repository）はそれを知らない。

### なぜ逆なのか

Go の interface は**構造的部分型**（structural typing）。`implements` 宣言が存在せず、メソッドセットが一致していれば自動的に満たす。この性質から3つの帰結が出る。

**(1) 実装側が利用側を import しなくて済む**  
提供側に interface を置くと `repository` パッケージに interface と実装が同居する。すると「repository を使いたいだけの service」が repository パッケージを import することになり、gorm への間接依存が service に持ち込まれる。利用側に置けば、依存の矢印は `service → (何も)`、`main → repository` で済む。

**(2) interface を最小にできる**  
必要なメソッドを知っているのは利用側だけ。もし repository 側に `PostRepository` interface を置くと「repository ができること全部」を並べたくなり、interface が肥大化する。

> The bigger the interface, the weaker the abstraction. — Rob Pike

`io.Reader` が1メソッドなのはこの原則の極致。

**(3) 後から切れる**  
既存の具象型に手を加えず、利用側で interface を宣言するだけで抽象化が成立する。今回の Day 2 → Day 3 のリファクタがまさにこれで、`PostRepo` 側には interface を意識した変更が1行も入っていない。

### 関連する定型句：Accept interfaces, return structs

```go
// 引数が interface、戻り値が具象型 — 定型句どおりの形
func NewPostService(repo postRepository) *PostService

// 引数は具象型（*gorm.DB は gorm が公開している struct）。戻り値だけが定型句に沿う
func NewPostRepo(db *gorm.DB) *PostRepo
```

- **引数が interface**：呼び出し側が何を渡すか自由になる（本物 / モック / キャッシュ付き実装）
- **戻り値が具象**：受け取った側が必要な形で interface を切れる。戻り値を interface にすると、後からメソッドを増やしても利用側から見えなくなる

### 「accept interfaces」が当てはまらない引数もある

`NewPostRepo` が `*gorm.DB` を具象型で受けているのは誤りではない。この定型句は**差し替えの余地がある依存**に対する指針であって、全引数を interface にせよという意味ではない。

判断基準は「その依存を**差し替える単位**はどこか」：

| 依存 | 受け取り方 | 理由 |
|---|---|---|
| service → repository | interface | 差し替える単位が repository そのもの（本物 / モック）。境界がここにある |
| repository → `*gorm.DB` | 具象型 | repository は gorm を**隠蔽する側**。gorm をやめるなら `PostRepo` ごと別実装に差し替わるので、`*gorm.DB` を interface で包んでも差し替え地点が増えない |

`*gorm.DB` を interface で抽象化しようとすると、gorm のチェーン API（`Find` / `First` / `Model` / `Where`…）をそのままなぞった巨大な interface になり、§2(2) の「interface は最小に」に真っ向から反する。抽象化の境界は**チェーンの途中ではなく、repository のメソッド境界に引く**のが正しい。

標準ライブラリでも `*sql.DB` は具象型のまま受け渡される。「依存＝すべて interface にする」は Go の流儀ではない。

### なぜ `postRepository` は小文字（非公開）なのか

exported にすると他パッケージから参照でき、メソッドを1本足すだけで外部のコンパイルが壊れうる。「service が repository をどう使うか」は service の内部事情なので、公開する理由がない。

一方 `PostService` が exported なのは、handler が具象型として受け取っているため。

```go
// internal/handler/post.go
type PostHandler struct {
	// serviceの差し替えは現状想定されていないので、interfaceによる抽象化は不要
	svc *service.PostService
}
```

**抽象化を入れない判断も設計判断**。差し替えの必要が生まれていない層に interface を置くと、実装が1つしかない interface（＝間接参照が増えるだけの層）が残る。必要になってから、handler 側に `postService` interface を切ればよい。

### 補足：`var _ I = (*T)(nil)` はどこに書くのか

interface 充足のコンパイル時チェックとして定番の書き方：

```go
var _ postRepository = (*repository.PostRepo)(nil)
```

これを**どこに書くか**が、consumer-side interface では問題になる。

**service パッケージに書く場合**：`repository` の import が必要になる。これは依存の逆流ではない（handler → service → repository は宣言している方向そのもの）し、循環 import にもならないので、**普通にコンパイルは通る**。禁止されている書き方ではない。

問題は、書いた瞬間に service の推移的依存が変わること。実測：

```
# 現状（service が import するのは model だけ）
$ go list -f '{{join .Deps "\n"}}' ./internal/service | grep -E '^gorm|database/sql'
（何も出ない）

# var _ のために repository を import した場合
$ go list -f '{{join .Deps "\n"}}' ./internal/service | grep -E '^gorm|database/sql'
database/sql
database/sql/driver
gorm.io/gorm
gorm.io/driver/mysql
gorm.io/gorm/clause
...
```

interface で分離した目的は「service が永続化技術を知らない状態」を作ることだったのに、**充足チェックの1行のためにそれを手放す**ことになる。書かない理由はここであって、「書けないから」ではない。

> ここは区別が重要：**コンパイラが禁止すること**（循環 import）と、**設計判断として避けること**（依存を1本増やす）は別物。前者は壊れるので気づけるが、後者は黙って通るので自分で守るしかない。`go list` で依存を機械的に見る（§0）意味はここにある。

**repository パッケージに書く場合**：これは本当に不可能。`postRepository` は非公開で参照できず、仮に公開しても `repository → service` の import が要る。**こちらが本物の逆流**であり、同時に循環 import になってコンパイルが止まる。

**では実際どこで検証されているか**：

```go
postService := service.NewPostService(postRepo) // ここ
```

`*PostRepo` を `postRepository` 引数へ渡す時点でコンパイラが照合する。**consumer-side interface では、配線コード（main）そのものが充足チェックになっている**ので、`var _` を足す動機がそもそも薄い。

実際、repository のシグネチャを変更（例：`ctx context.Context` を追加）して service 側の interface を直し忘れると、エラーはこう出る：

```
cmd/api-server/main.go:30:40: cannot use postRepo (variable of type *repository.PostRepo)
  as service.postRepository value in argument to service.NewPostService:
  *repository.PostRepo does not implement service.postRepository (wrong type for method Create)
        have Create(context.Context, model.Post) (uint, error)
        want Create(model.Post) (uint, error)
```

`var _` が要るのは「実装側パッケージに置いて、利用側とは独立にビルドを守りたい」場合（＝provider-side interface を採る設計や、公開ライブラリとして実装を提供する場合）。今回のような consumer-side interface では出番がない。

---

## 3. main = composition root

§2 が「層の間にどう境界を引くか」の話だったのに対して、ここは「**具象型を誰が選ぶか**」の話。Day 2 では選択の主体が散っていた。

### before：グローバル変数は宣言・代入・利用が3箇所に散る

```go
// internal/store/phase2.go
var DB *gorm.DB                    // ① 宣言：どのパッケージからでも書き換えられる

func InitMySQL(cfg *Config) error {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{...})
	if err != nil {
		return fmt.Errorf("failed to open: %v", err)
	}
	DB = db                        // ② 代入：戻り値ではなく副作用として起きる
	return DB.AutoMigrate(&User{}, &Post{}, &Tag{})
}

// internal/handler/phase2.go — handler が直接グローバルを掴む
func GetPosts(w http.ResponseWriter, r *http.Request) {
	var posts []store.Post
	if err := store.DB.Find(&posts).Error; err != nil { ... }   // ③ 利用
}

// cmd/api-server/main.go
if err := store.InitMySQL(cfg); err != nil { ... }   // 戻り値は error だけ
                                                     // 「どこかに」DBが用意される
```

`InitMySQL` が「接続を開く」「グローバルに代入する」「マイグレーションする」を1関数で兼ねていて、**DB という値そのものは呼び出し側に一度も渡らない**。main を読んでも、どの handler がどの DB を使うのかは分からない。

### after：値が main を起点に一本の連鎖で流れる

```go
// internal/repository/db.go — 開いて「返す」だけ
func InitMySQL(cfg *Config) (*gorm.DB, error) { ... }

// internal/repository/post.go — 依存が型定義に書いてある
type PostRepo struct{ db *gorm.DB }
func NewPostRepo(db *gorm.DB) *PostRepo { return &PostRepo{db: db} }

func (r *PostRepo) FindAll() ([]model.Post, error) {
	var posts []model.Post
	if err := r.db.Find(&posts).Error; err != nil { ... }
}

// cmd/api-server/main.go — 具象型を選ぶのはここだけ
db, err := repository.InitMySQL(cfg)          // *gorm.DB を選ぶ
postRepo := repository.NewPostRepo(db)        // *PostRepo を選ぶ
postService := service.NewPostService(postRepo)
postHandler := handler.NewPostHandler(postService)
router := handler.NewRouter(postHandler)
```

①②③ が `db → postRepo → postService → postHandler` という**1本の連鎖に畳まれて main に集約された**。これが composition root（合成の起点）で、依存関係の組み立てを1箇所でやりきる場所を指す。

「MySQL を使う」「gorm を使う」は業務ロジックではなく**実行時の構成**なので、業務ロジックの中では決めない。`PostRepo` は「`*gorm.DB` を1つもらう」としか言っておらず、それがどこの MySQL かを知らない。

> `InitMySQL` から `AutoMigrate` を剥がして `repository/seed.go` に移したのも同じ流れ。「接続を開く」と「スキーマを作る」は別の関心事で、後者は main が呼ぶかどうかを選べるべきもの。

### グローバル変数をやめると何が変わるか

| | グローバル変数 | コンストラクタ注入 |
|---|---|---|
| 依存の可視性 | 関数本体を読まないと分からない | 型定義に書いてある |
| 初期化順序 | 暗黙（`init` / 代入のタイミング） | main で明示、未初期化なら nil で即死 |
| テスト | 差し替え不能（グローバルを書き換えるしかない） | 引数を変えるだけ |
| 同時に2つ持つ | 不可能（read/write 分離 DB など） | 可能 |

「初期化順序」の行が実務では効く。グローバル変数の形だと、`store.InitMySQL` を呼ぶ前に `store.DB` を使うコードが書けてしまい、**コンパイルは通って実行時に nil pointer で落ちる**。注入の形にすると、`NewPostRepo(db)` に渡す `db` を先に作るしかないので、順序が式の構造として強制される。

---

## 4. エラーを層の境界で翻訳する

「handler が gorm を import していない」を実際に成立させているのはこの仕組み。

```go
// internal/model/errors.go — 全層が import できる最下層に置く
var (
	ErrPostNotFound         = errors.New("post not found")
	ErrPostHasInvalidUserID = errors.New("post has invalid userID")
	ErrInvalidPostInput     = errors.New("body and userID cannot be empty")
)
```

```go
// internal/repository/post.go — gorm の語彙 → model の語彙
func (r *PostRepo) FindByID(id uint) (*model.Post, error) {
	if err := r.db.First(&post, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to find Post ID[%v]: %w", id, model.ErrPostNotFound)
		}
		return nil, fmt.Errorf("failed to get Post ID[%v]: %v", id, err)
	}
	return &post, nil
}
```

```go
// internal/handler/post.go — model の語彙 → HTTP の語彙
post, err := h.svc.GetPost(id)
if err != nil {
	if errors.Is(err, model.ErrPostNotFound) {
		http.Error(w, "Client error: no such Post", http.StatusNotFound)
		return
	}
	http.Error(w, "Server error: failed to get Post", http.StatusInternalServerError)
	return
}
```

各層が話す言葉：

| 層 | エラーの語彙 |
|---|---|
| repository | `gorm.ErrRecordNotFound`, `gorm.ErrForeignKeyViolated` |
| service / handler | `model.ErrPostNotFound`, `model.ErrPostHasInvalidUserID` |
| クライアント | `404`, `400`, `500` |

**翻訳を挟まないとどうなるか**：handler が `errors.Is(err, gorm.ErrRecordNotFound)` を書く → handler が gorm を import する → 層分離が名前だけになる。エラーの型は依存を運ぶ経路なので、境界で必ず詰め替える。

### `%w` と `%v` の使い分け

```go
return nil, fmt.Errorf("...: %w", id, model.ErrPostNotFound) // 判定させたい
return nil, fmt.Errorf("...: %v", id, err)                    // 判定させない
```

- `%w` は元のエラーを**チェーンとして保持**する。`errors.Is` / `errors.As` が辿れる
- `%v` は文字列に潰す。ログには残るが `errors.Is` では引っかからない

現在のコードは「呼び出し側が分岐する必要があるものだけ `%w`、それ以外は `%v`」になっている。これは意図的に妥当な使い分け。全部 `%w` にすると、内部実装のエラー型が上位層の判定対象として**公開 API になってしまう**（gorm を差し替えたら上位が壊れる）。

### sentinel error を model に置いた判断

`model` はどの層も import できる（依存グラフの底）ので、共通語彙の置き場所として自然。代替案は「service パッケージに置く」だが、repository が service を import できないので不可能。**依存グラフが置き場所を決めている**。

---

## 5. HTTP の語彙を handler に閉じ込める

```go
// すべて handler パッケージの非公開型
type postsOutput struct {
	ID        uint      `json:"id"`
	UserID    uint      `json:"userId"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type createPostInput struct { UserID uint `json:"userId"`; Body string `json:"body"` }
type updatePostInput struct { Body string `json:"body"` }

func validateID(w http.ResponseWriter, r *http.Request) (uint, bool)
func initDecoder(w http.ResponseWriter, r *http.Request) *json.Decoder
```

判定基準は **service のシグネチャに `net/http` の型が1つも現れないこと**。

```go
func (s *PostService) ListPost() ([]model.Post, error)
func (s *PostService) GetPost(id uint) (*model.Post, error)
func (s *PostService) CreatePost(post model.Post) (uint, error)
func (s *PostService) UpdatePost(id uint, body string) error
func (s *PostService) DeletePost(id uint) error
```

`http.ResponseWriter` も `*http.Request` も `json` タグも出てこない。この状態だと、

- service を CLI / gRPC / バッチから同じように呼べる
- service のテストに `httptest` が要らない（ただの関数呼び出しになる）

`UpdatePost(id uint, body string)` が `model.Post` ではなく素の値を取っているのも、「更新できるのは body だけ」という契約を型で表現していて良い。`model.Post` を渡す形にすると、service 内で「どのフィールドを見るか」の暗黙ルールが必要になる。

---

## 6. 業務ロジックと HTTP の切り分け方

Day 2 は1つの `if` で両方を判定していた：

```go
// Day 2（before）
err := initDecoder(w, r).Decode(&in)
if err != nil || in.Body == "" || in.UserID == 0 {   // デコード失敗と業務ルール違反が同居
	http.Error(w, "Client error: sent invalid Post data", http.StatusBadRequest)
	return
}
```

```go
// Day 3（after）: handler
var in createPostInput
if err := initDecoder(w, r).Decode(&in); err != nil {   // ← JSON が壊れている（HTTP/JSON の問題）
	http.Error(w, "Client error: sent invalid reqeust", http.StatusBadRequest)
	return
}
postID, err := h.svc.CreatePost(model.Post{UserID: in.UserID, Body: in.Body})
if err != nil {
	if errors.Is(err, model.ErrInvalidPostInput) { ... 400 }
	if errors.Is(err, model.ErrPostHasInvalidUserID) { ... 400 }
	... 500
}
```

```go
// Day 3（after）: service
func (s *PostService) CreatePost(post model.Post) (uint, error) {
	// 業務ルール上の制約(ex. Postは必ず投稿者に紐づき、空にならない）はserviceで検証
	if post.Body == "" || post.UserID == 0 {
		return 0, model.ErrInvalidPostInput
	}
	return s.repo.Create(post)
}
```

**どちらの層に置くかの判断基準**：*そのルールは、入力が HTTP でなくても成り立つか？*

| ルール | 層 | 理由 |
|---|---|---|
| JSON がパースできない | handler | JSON という表現形式に固有 |
| 未知フィールドが含まれる（`DisallowUnknownFields`） | handler | 同上 |
| リクエストが 1KB を超える（`MaxBytesReader`） | handler | HTTP 固有の防御 |
| path parameter が整数でない | handler | URL という表現形式に固有 |
| Post の body が空であってはならない | service | CLI から作っても成り立つ |
| Post は必ず既存 User に紐づく | service（+DB の FK 制約） | ドメインのルール |

外形的な結果として、Day 2 では一括 400 だったものが、curl 検証では原因別のメッセージに分かれた（ステータスコードは不変）：

```
POST body 空          → 400 Client error: sent invalid Post: field cannot be empty
POST 未知field        → 400 Client error: sent invalid reqeust
POST 存在しないuserId → 400 Client error: sent invalid Post: no such userId
```

リファクタで**振る舞い（ステータスコード）は変えずに、責務だけ移動できている**ことの確認になる。

---

## 7. 命名：Go の stutter を意識する

```go
service.PostService   // ← package 名と型名が重複（stutter）
repository.PostRepo
```

Go では利用側から見た**完全修飾名**で読みやすさを判断する慣習がある（`http.Server`、`bytes.Buffer`。`http.HTTPServer` とは書かない）。この基準だと `service.Post` / `repository.Post` が Go らしい。

ただし今回の構成では `model.Post` があるため、`service.Post` はドメイン型と紛らわしい。実務では次のどちらかに寄せることが多い：

| 切り方 | 例 |
|---|---|
| 層でパッケージを切る（今回） | `service.PostService` — stutter を許容して曖昧さを避ける |
| 機能でパッケージを切る | `post.Service` / `post.Repository` / `post.Post`→`post.Model` |

今回は層で切っているので現状の命名で問題ない。**知っておくべきは「Go では package 名も識別子の一部」という原則**の方。

---

## 8. 意図的にやっていないこと（スコープ外の記録）

| 項目 | 現状 | 影響 |
|---|---|---|
| ドメインモデルと永続化モデルの分離 | `model.Post` が `gorm.Model` を埋め込み、`gorm:"not null"` タグを持つ | `model` が gorm を import しているので、service / handler も**間接的には** gorm に依存している。「handler が gorm を直接 import しない」は満たすが、ORM の都合（`ID` / `DeletedAt` / タグ）がドメイン型に漏れている |
| 入出力 DTO と service の分離 | handler が `model.Post` を組み立てて service に渡す | 永続化モデルが handler まで届いている |
| context 伝播 | メソッドが `ctx` を受け取らない | タイムアウト・キャンセルが DB クエリまで届かない（Day 4） |
| トランザクション境界 | repository のメソッド単位＝暗黙の1トランザクション | 複数 repository をまたぐ操作が原子的にできない |
| Phase1 handler の層分離 | `handler/phase1.go` が `repository` を直 import | 層の飛び越しが1本残置（省力化の判断） |

1行目が構造的には最も大きい。「handler が gorm を import していない」というチェックは**必要条件であって十分条件ではない**。ドメイン型が ORM 由来の型を持っている限り、ORM の差し替えはドメイン層に波及する。ここを切るには model を素の struct にして repository 側に gorm 用の型と変換関数を置く必要があるが、記述量が一気に増えるので、規模とのトレードオフで判断する領域。

---

## 9. この形が Day 5（テスト）にどう効くか

`postRepository` が非公開・小さい・DB を知らない、という状態になったので、手書きモックで満たせる：

```go
// service パッケージ内のテストファイルに置ける（postRepository が非公開でも同一パッケージなら見える）
type stubPostRepo struct {
	create func(post model.Post) (uint, error)
}

func (s *stubPostRepo) Create(post model.Post) (uint, error) { return s.create(post) }
func (s *stubPostRepo) FindAll() ([]model.Post, error)       { return nil, nil }
func (s *stubPostRepo) FindByID(id uint) (*model.Post, error) { return nil, nil }
func (s *stubPostRepo) Update(id uint, body string) error     { return nil }
func (s *stubPostRepo) Delete(id uint) error                  { return nil }
```

- **フィールドに関数を持たせる**のがテーブルドリブンとの相性が良い。ケースごとに `create` の中身だけ差し替えれば、同じ stub 型で全ケースを回せる
- interface のメソッド本数がそのままモックの記述量になる。**interface を最小にする理由がここでも効く**
- service のテストに MySQL が要らない ＝ CI で `go test ./...` だけで回る

---

## まとめ：今日入った「型」

1. 層はファイルではなく**パッケージ**で切る。循環 import の禁止が依存方向を強制する
2. interface は**利用側**に、**非公開**で、**最小限**に定義する
3. 具象型の選択は **main 1箇所**に集約する（composition root）
4. エラーは**層の境界で翻訳**する。`%w` は「上位に判定させたいものだけ」
5. 各層の**シグネチャに他層の語彙が現れない**ことをチェックポイントにする
6. 抽象化を**入れない判断**も設計判断。差し替えの必要が生まれてから interface を切る
