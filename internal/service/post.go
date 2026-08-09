package service

import (
	"context"
	"database/sql"

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

func NewPostService(repo postRepository) *PostService {
	return &PostService{repo: repo}
}

// PostService はinterface(postRepository)を経由してrepositoryへアクセスする
// handler -> serviceにおいてはservice差し替えは未定で、具象型として直接呼ばれるので、exportする
type PostService struct {
	repo postRepository
}

func (s *PostService) ListPost(ctx context.Context) ([]model.Post, error) {
	return s.repo.FindAll(ctx)
}

func (s *PostService) GetPost(ctx context.Context, id uint) (*model.Post, error) {
	return s.repo.FindByID(ctx, id)
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
	return s.repo.Update(ctx, id, body)
}

func (s *PostService) DeletePost(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

func (s *PostService) Sleep(ctx context.Context, sec int) error {
	return s.repo.Sleep(ctx, sec)
}

func (s *PostService) DBStats() (sql.DBStats, error) {
	return s.repo.Stats()
}
