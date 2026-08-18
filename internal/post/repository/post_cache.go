package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/soomattsu/http-server-practice/internal/model"
)

// 実務上必須（データ更新時のcache無効化が失敗した場合、TTLが無いと永久にstale cacheが返り続けるため）
const postCacheTTL = 60 * time.Second

type PostCache struct {
	client *redis.Client
}

func NewPostCache(client *redis.Client) *PostCache {
	return &PostCache{client: client}
}

func postKey(id uint) string {
	return fmt.Sprintf("post:%v", id)
}

/*
 * relation込みでデータをキャッシュする場合、データ本体だけでなく、relationの更新もキャッシュに反映する必要が生じる
 * （ex. Post.Bodyは変更無しでも、Post.Tagsの変更があった場合、それを反映できていなければstale cacheになってしまう）
 * どこまでキャッシュに含めるかは、"そのキャッシュ経由でデータを返すAPIの仕様次第"で決まる
 * （PostCacheにおいてはPost.Tagsはスコープアウト。PostのCreate/Read APIでTagsを扱わないため。）
 */

func (c *PostCache) GetPost(ctx context.Context, id uint) (*model.Post, error) {
	b, err := c.client.Get(ctx, postKey(id)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, model.ErrCacheMiss
		}
		return nil, fmt.Errorf("failed to get cache for Post ID[%v]: %v", id, err)
	}
	var post model.Post
	if err := json.Unmarshal(b, &post); err != nil {
		return nil, fmt.Errorf("failed to decode cached Post ID[%v]: %v", id, err)
	}
	return &post, nil
}

func (c *PostCache) SetPost(ctx context.Context, post *model.Post) error {
	b, err := json.Marshal(post)
	if err != nil {
		return fmt.Errorf("failed to encode Post ID[%v] to cache: %v", post.ID, err)
	}
	if err := c.client.Set(ctx, postKey(post.ID), b, postCacheTTL).Err(); err != nil {
		return fmt.Errorf("failed to set cache for Post ID[%v]: %v", post.ID, err)
	}
	return nil
}

func (c *PostCache) DeletePost(ctx context.Context, id uint) error {
	if err := c.client.Del(ctx, postKey(id)).Err(); err != nil {
		return fmt.Errorf("failed to delete cache for Post ID[%v]: %v", id, err)
	}
	return nil
}
