package service

import (
	"context"
	"database/sql"
	"errors"
	"log"

	"github.com/soomattsu/http-server-practice/internal/model"
)

// postRepository は Post用のserviceがrepositoryを"利用"する時に必要なメソッドを宣言したinterface。
type postRepository interface {
	FindAll(ctx context.Context) ([]model.Post, error)
	// FindByID はレコードが見つからなかった場合model.ErrPostNotFoundを返す。
	FindByID(ctx context.Context, id uint) (*model.Post, error)
	// Create はレコード作成時に外部キー制約に違反した場合model.ErrPostHasInvalidUserIDを返す。
	Create(ctx context.Context, post model.Post) (uint, error)
	// Update はSELECT -> UPDATEを行い、レコードが見つからなった場合場合model.ErrPostNotFoundを返す。
	Update(ctx context.Context, id uint, body string) error
	Delete(ctx context.Context, id uint) error

	Sleep(ctx context.Context, sec int) error
	Stats() (sql.DBStats, error)
}

type postCache interface {
	// GetPost はキャッシュヒットしなかった場合model.ErrCacheMissを返す
	GetPost(ctx context.Context, id uint) (*model.Post, error)
	SetPost(ctx context.Context, post *model.Post) error
	DeletePost(ctx context.Context, id uint) error
}

// PostService はinterface(postRepository, postCache)を経由してrepositoryへアクセスする
// handler -> serviceにおいてはservice差し替えは未定で、具象型として直接呼ばれるので、exportする
type PostService struct {
	repo  postRepository
	cache postCache
}

func NewPostService(repo postRepository, cache postCache) *PostService {
	return &PostService{repo: repo, cache: cache}
}

func (s *PostService) ListPost(ctx context.Context) ([]model.Post, error) {
	return s.repo.FindAll(ctx)
}

// cache-asideパターン（lazy load: 参照時にcache missした際にcacheへ読み込む）を実装
func (s *PostService) GetPost(ctx context.Context, id uint) (*model.Post, error) {
	// cache経由の取得を試行
	cached, err := s.cache.GetPost(ctx, id)
	switch {
	case err == nil:
		log.Printf("cache HIT: post:%v", id)
		return cached, nil
	case errors.Is(err, model.ErrCacheMiss):
		// Postを返すのがcache経由かRDB経由は上流のhandlerには関係ないので、ErrCacheMissはserviceで処理する
		log.Printf("cache MISS: post:%v", id)
	default:
		// cacheが単に読み取り高速化用に置かれている場合は、落ちていた場合にDBへフォールバックする設計(fail open)でOK
		// DBがcache前提でサイジングされている場合は、cacheが落ちた場合にDBが全トラフィックを扱えない
		// - リクエスト失敗(fail close)させたり、DBへのフォールバックの流量制限を行う必要がある
		log.Printf("cache ERROR: post:%v: %v", id, err)
	}

	// cacheから取得できなかったらRDBから取得する
	post, err := s.repo.FindByID(ctx, id)
	if err != nil {
		// 実務では、cache penetration(存在しないIDのGETを大量に投げることでDBに負荷をかける攻撃)への考慮が必要
		// 「404の場合に空の値を短いTTLでキャッシュする」などで（トレードオフ付きで）予防できる
		return nil, err
	}
	// ここでキャッシュ書き込みに失敗しても、次回のキャッシュ参照がmissするだけなので、リクエスト全体は成功させる
	if err := s.cache.SetPost(ctx, post); err != nil {
		log.Printf("cache SET failed: post:%v: %v", id, err)
	}
	return post, nil
}

func (s *PostService) CreatePost(ctx context.Context, post model.Post) (uint, error) {
	// 業務ルール上の制約(ex. Postは必ず投稿者に紐づき、空にならない）はserviceで検証
	if post.Body == "" || post.UserID == 0 {
		return 0, model.ErrInvalidPostInput
	}
	return s.repo.Create(ctx, post)
}

func (s *PostService) UpdatePost(ctx context.Context, id uint, body string) error {
	// 業務ルール上の制約(ex. Postは空にならない）はserviceで検証
	if body == "" {
		return model.ErrInvalidPostInput
	}
	if err := s.repo.Update(ctx, id, body); err != nil {
		return err
	}
	/*
		データ更新によって古くなったcacheを無効化する
		必ず"データ更新完了後"にcacheを消す
		先に消すと、同時に別reqを受けてReadした場合「cache miss->古いデータをcacheへ格納」が生じ、更新後にも関わらずstale cacheが残るため
	*/
	s.invalidatePostCache(ctx, id)
	return nil
}

func (s *PostService) DeletePost(ctx context.Context, id uint) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	/*
		データ更新によって古くなったcacheを無効化する
		必ず"データ更新完了後"にcacheを消す
		先に消すと、同時に別reqを受けてReadした場合「cache miss->古いデータをcacheへ格納」が生じ、更新後にも関わらずstale cacheが残るため
	*/
	s.invalidatePostCache(ctx, id)
	return nil
}

func (s *PostService) Sleep(ctx context.Context, sec int) error {
	return s.repo.Sleep(ctx, sec)
}

func (s *PostService) DBStats() (sql.DBStats, error) {
	return s.repo.Stats()
}

func (s *PostService) invalidatePostCache(ctx context.Context, id uint) {
	// cache削除失敗時は、ログを出すだけでリクエスト全体は成功させる
	// これは、TTL経過までstale cacheが返り続けることを許容するのと同義
	if err := s.cache.DeletePost(ctx, id); err != nil {
		log.Printf("cache DEL failed: post:%v: %v", id, err)
	}
}
