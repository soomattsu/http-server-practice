package repository

import (
	"errors"
	"fmt"

	"github.com/soomattsu/http-server-practice/internal/model"
	"gorm.io/gorm"
)

type PostRepo struct {
	db *gorm.DB
}

func NewPostRepo(db *gorm.DB) *PostRepo {
	return &PostRepo{db: db}
}

func (r *PostRepo) FindAll() ([]model.Post, error) {
	var posts []model.Post
	if err := r.db.Find(&posts).Error; err != nil {
		return nil, fmt.Errorf("failed to find Posts: %v", err)
	}
	return posts, nil
}

func (r *PostRepo) FindByID(id uint) (*model.Post, error) {
	var post model.Post
	if err := r.db.First(&post, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to find Post ID[%v]: %w", id, model.ErrPostNotFound)
		}
		return nil, fmt.Errorf("failed to get Post ID[%v]: %v", id, err)
	}
	return &post, nil
}

func (r *PostRepo) Create(post model.Post) (uint, error) {
	if err := r.db.Create(&post).Error; err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			return 0, fmt.Errorf("failed to create Post w/ UserID[%v]: %w", post.UserID, model.ErrPostHasInvalidUserID)
		}
		return 0, fmt.Errorf("failed to create Post: %v", err)
	}
	return post.ID, nil
}

func (r *PostRepo) Update(id uint, body string) error {
	// SELECT -> UPDATE
	var post model.Post
	if err := r.db.First(&post, id).Error; err != nil {
		// UPDATE対象レコードが取れなかったら即エラー
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to find Post ID[%v] to UPDATE: %w", id, model.ErrPostNotFound)
		}
		return fmt.Errorf("failed to get Post ID[%v]: %v", id, err)
	}
	// SELECTに使ったFirst()は"Finisher Method"なので、チェーンしてUpdates()すべきではない
	// Updates()の引数にデータモデル以外のstructを渡すと、updated_atの自動更新が効かない！！
	if err := r.db.Model(&post).Updates(model.Post{Body: body}).Error; err != nil {
		return fmt.Errorf("failed to update Post ID[%v]: %v", id, err)
	}
	return nil
}

func (r *PostRepo) Delete(id uint) error {
	if err := r.db.Delete(&model.Post{}, id).Error; err != nil {
		return fmt.Errorf("failed to delete Post ID[%v]: %v", id, err)
	}
	return nil
}
