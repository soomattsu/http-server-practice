package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/soomattsu/http-server-practice/internal/model"
	"gorm.io/gorm"
)

// コンストラクタ（NewPostRepo）がcomposite rootから呼ばれる想定でexportされているので、それが返す具象型もexportする
// unexportしてもコンパイルは通るが、not idiomaticな書き方
type PostRepo struct {
	db *gorm.DB
}

func NewPostRepo(db *gorm.DB) *PostRepo {
	return &PostRepo{db: db}
}

/*
 * Postを取得する時にPost.Tagsを含めるかどうかは、APIの仕様次第
 * 慣例的には、relation fieldの扱いは以下の通りか
 * - FindAll: 含めないことが多い。Post全件のTagsを併せて返すことになりペイロードが膨らむ。
 * - FindByID: 含めることが多い。一件のPostの詳細・全情報を返すためのメソッドなので。
 */

func (r *PostRepo) FindAll(ctx context.Context) ([]model.Post, error) {
	var posts []model.Post
	// gormのWithContext(ctx) は、database/sqlのQueryContextやExecContextへctxを橋渡しする
	// これらは内部で受け取ったctxのDone()を監視していて、Doneになった瞬間に処理を中断してエラーを返す
	if err := r.db.WithContext(ctx).Find(&posts).Error; err != nil {
		return nil, fmt.Errorf("failed to find Posts: %v", err)
	}
	return posts, nil
}

func (r *PostRepo) FindByID(ctx context.Context, id uint) (*model.Post, error) {
	var post model.Post
	if err := r.db.WithContext(ctx).First(&post, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to find Post ID[%v]: %w", id, model.ErrPostNotFound)
		}
		return nil, fmt.Errorf("failed to get Post ID[%v]: %v", id, err)
	}
	return &post, nil
}

func (r *PostRepo) Create(ctx context.Context, post model.Post) (uint, error) {
	if err := r.db.WithContext(ctx).Create(&post).Error; err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			return 0, fmt.Errorf("failed to create Post w/ UserID[%v]: %w", post.UserID, model.ErrPostHasInvalidUserID)
		}
		return 0, fmt.Errorf("failed to create Post: %v", err)
	}
	return post.ID, nil
}

func (r *PostRepo) Update(ctx context.Context, id uint, body string) error {
	// SELECT -> UPDATE
	var post model.Post
	if err := r.db.WithContext(ctx).First(&post, id).Error; err != nil {
		// UPDATE対象レコードが取れなかったら即エラー
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to find Post ID[%v] to UPDATE: %w", id, model.ErrPostNotFound)
		}
		return fmt.Errorf("failed to get Post ID[%v]: %v", id, err)
	}
	// SELECTに使ったFirst()は"Finisher Method"なので、チェーンしてUpdates()すべきではない
	// Updates()の引数にデータモデル以外のstructを渡すと、updated_atの自動更新が効かない！！
	if err := r.db.WithContext(ctx).Model(&post).Updates(model.Post{Body: body}).Error; err != nil {
		return fmt.Errorf("failed to update Post ID[%v]: %v", id, err)
	}
	return nil
}

func (r *PostRepo) Delete(ctx context.Context, id uint) error {
	if err := r.db.WithContext(ctx).Delete(&model.Post{}, id).Error; err != nil {
		return fmt.Errorf("failed to delete Post ID[%v]: %v", id, err)
	}
	return nil
}

func (r *PostRepo) Sleep(ctx context.Context, sec int) error {
	return r.db.WithContext(ctx).Exec("SELECT SLEEP(?)", sec).Error
}

func (r *PostRepo) Stats() (sql.DBStats, error) {
	sqlDB, err := r.db.DB()
	if err != nil {
		return sql.DBStats{}, fmt.Errorf("failed to get *sql.DB: %v", err)
	}
	return sqlDB.Stats(), nil
}
