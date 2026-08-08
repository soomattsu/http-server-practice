package service

import "github.com/soomattsu/http-server-practice/internal/model"

// postRepository は Post用のserviceがrepositoryを"利用"する時に必要なメソッドを宣言したinterface。
type postRepository interface {
	FindAll() ([]model.Post, error)
	// FindByID はレコードが見つからなかった場合model.ErrPostNotFoundを返す。
	FindByID(id uint) (*model.Post, error)
	// Create はレコード作成時に外部キー制約に違反した場合model.ErrPostHasInvalidUserIDを返す。
	Create(post model.Post) (uint, error)
	// Update はSELECT -> UPDATEを行い、レコードが見つからなった場合場合model.ErrPostNotFoundを返す。
	Update(id uint, body string) error
	Delete(id uint) error
}

func NewPostService(repo postRepository) *PostService {
	return &PostService{repo: repo}
}

// PostService はinterface(postRepository)を経由してrepositoryへアクセスする
// handler -> serviceにおいてはservice差し替えは未定で、具象型として直接呼ばれるので、exportする
type PostService struct {
	repo postRepository
}

func (s *PostService) ListPost() ([]model.Post, error) {
	return s.repo.FindAll()
}

func (s *PostService) GetPost(id uint) (*model.Post, error) {
	return s.repo.FindByID(id)
}

func (s *PostService) CreatePost(post model.Post) (uint, error) {
	// 業務ルール上の制約(ex. Postは必ず投稿者に紐づき、空にならない）はserviceで検証
	if post.Body == "" || post.UserID == 0 {
		return 0, model.ErrInvalidPostInput
	}
	return s.repo.Create(post)
}

func (s *PostService) UpdatePost(id uint, body string) error {
	// 業務ルール上の制約(ex. Postは空にならない）はserviceで検証
	if body == "" {
		return model.ErrInvalidPostInput
	}
	return s.repo.Update(id, body)
}

func (s *PostService) DeletePost(id uint) error {
	return s.repo.Delete(id)
}
