package repository

import (
	"context"

	"github.com/soomattsu/http-server-practice/internal/model"
)

// NoopPostCache はRedisが利用できない環境で、PostCacheの代わりにinjectionされる。
// キャッシュ参照時は、常にmodel.ErrCacheMissを返すことで、読み取り経路をRDBのみに限定する。
// 更新系は、No-opにおいては"正常系=何もしない"なのでnilを返す。
type NoopPostCache struct{}

func NewNoopPostCache() *NoopPostCache {
	return &NoopPostCache{}
}

func (c *NoopPostCache) GetPost(ctx context.Context, id uint) (*model.Post, error) {
	return nil, model.ErrCacheMiss
}

func (c *NoopPostCache) SetPost(ctx context.Context, post *model.Post) error {
	return nil
}

func (c *NoopPostCache) DeletePost(ctx context.Context, id uint) error {
	return nil
}
