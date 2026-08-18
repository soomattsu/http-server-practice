// ホワイトボックステスト（unexportedな関数・変数にもアクセス可能）の場合は、対象と同じパッケージに入れる
// ブラックボックステスト（exportedなシンボルしか使えない）の場合は、"package *_test"に入れる

package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/soomattsu/http-server-practice/internal/model"
)

// mockPostRepository はpostRepository interfaceを満たす手書きモック
// 振る舞いを差し替えられるように、テストケースが依存するpostRepositoryのメソッドをフィールドに持つ
// （PostService.GetPostがテスト対象なので、それが依存するpostRepository.FindByIDを差し替え可能にする）
type mockPostRepository struct {
	findByIDFunc  func(ctx context.Context, id uint) (*model.Post, error)
	findByIDCalls int
}

// *mockPostRepositoryがpostRepository interfaceを満たしていることをコンパイル時に検証する
var _ postRepository = (*mockPostRepository)(nil)

func (m *mockPostRepository) FindByID(ctx context.Context, id uint) (*model.Post, error) {
	m.findByIDCalls++
	return m.findByIDFunc(ctx, id)
}

/*
 * モックがpostRepository interfaceを満たすための書き捨てメソッド
 * 差し替え対象メソッド以外も含むすべてのメソッドを実装する必要がある
 * -> interfaceが肥大化した際のデメリットの一つ
 */

func (m *mockPostRepository) FindAll(ctx context.Context) ([]model.Post, error) {
	panic("FindAll: not implemented")
}

func (m *mockPostRepository) Create(ctx context.Context, post model.Post) (uint, error) {
	panic("Create: not implemented")
}

func (m *mockPostRepository) Update(ctx context.Context, id uint, body string) error {
	panic("Update: not implemented")
}

func (m *mockPostRepository) Delete(ctx context.Context, id uint) error {
	panic("Delete: not implemented")
}

func (m *mockPostRepository) Sleep(ctx context.Context, sec int) error {
	panic("Sleep: not implemented")
}

func (m *mockPostRepository) Stats() (sql.DBStats, error) {
	panic("Stats: not implemented")
}

// mockPostCache はpostCache interfaceを満たす手書きモック
// 振る舞いを差し替えられるように、テストケースが依存するpostCacheのメソッドをフィールドに持つ
// （PostService.GetPostがテスト対象なので、それが依存するpostCache.GetPostを差し替え可能にする）
type mockPostCache struct {
	getPostFunc     func(ctx context.Context, id uint) (*model.Post, error)
	setPostCalls    int
	deletePostCalls int
}

// *mockPostCacheがpostCache interfaceを満たしていることをコンパイル時に検証する
var _ postCache = (*mockPostCache)(nil)

func (m *mockPostCache) GetPost(ctx context.Context, id uint) (*model.Post, error) {
	return m.getPostFunc(ctx, id)
}

func (m *mockPostCache) SetPost(ctx context.Context, post *model.Post) error {
	m.setPostCalls++
	return nil
}

func (m *mockPostCache) DeletePost(ctx context.Context, id uint) error {
	m.deletePostCalls++
	return nil
}

// table-driven test：テストケースをデータとして並べ、1つの検証ロジックでループしてテストする方法
func TestPostService_GetPost(t *testing.T) {
	wantPost := &model.Post{Body: "cached body"}
	dbPost := &model.Post{Body: "db body"}

	// 匿名structのスライスのリテラルで、テストケースを表現する
	tests := []struct {
		// テストケース名
		name string
		// モックするrepository層の振る舞い
		// テスト実行時、指定した関数を使って各ケースのモックを初期化することで、repositoryのメソッドを差し替える
		cacheGet func(ctx context.Context, id uint) (*model.Post, error)
		repoFind func(ctx context.Context, id uint) (*model.Post, error)
		// 期待値
		wantBody      string
		wantErr       error // errors.Is比較用
		wantRepoCalls int   // repositoryへ到達した回数
		wantSetCalls  int   // キャッシュ書き込み回数
	}{
		{
			name: "1. cache HIT: repositoryを呼ばずにキャッシュの値を返す",
			cacheGet: func(ctx context.Context, id uint) (*model.Post, error) {
				return wantPost, nil
			},
			repoFind:      nil, // このケースでは呼ばれない想定
			wantBody:      "cached body",
			wantErr:       nil,
			wantRepoCalls: 0,
			wantSetCalls:  0,
		},
		{
			name: "2. cache MISS: repositoryを呼んで値を取得し、キャッシュへ格納して返す",
			cacheGet: func(ctx context.Context, id uint) (*model.Post, error) {
				return nil, model.ErrCacheMiss
			},
			repoFind: func(ctx context.Context, id uint) (*model.Post, error) {
				return dbPost, nil
			},
			wantBody:      "db body",
			wantErr:       nil,
			wantRepoCalls: 1,
			wantSetCalls:  1,
		},
		{
			name: "3. cache MISS: repositoryがmodel.ErrPostNotFoundを返す（異常系）",
			cacheGet: func(ctx context.Context, id uint) (*model.Post, error) {
				return nil, model.ErrCacheMiss
			},
			repoFind: func(ctx context.Context, id uint) (*model.Post, error) {
				return nil, fmt.Errorf("failed to find Post ID[%v]: %w", id, model.ErrPostNotFound)
			},
			wantBody:      "",
			wantErr:       model.ErrPostNotFound,
			wantRepoCalls: 1,
			wantSetCalls:  0,
		},
		{
			name: "4. cache ERROR: repositoryへフォールバック（fail open）して値を取得して返す",
			cacheGet: func(ctx context.Context, id uint) (*model.Post, error) {
				return nil, fmt.Errorf("failed to get cache for Post ID[%v]", id) // default節に入る任意のエラー
			},
			repoFind: func(ctx context.Context, id uint) (*model.Post, error) {
				return dbPost, nil
			},
			wantBody:      "db body",
			wantErr:       nil,
			wantRepoCalls: 1,
			wantSetCalls:  1,
		},
	}

	for _, tt := range tests {
		// 共通の検証ロジックを各ケースに対して回す
		// t.Run()によるサブテストとして実行することで、各ケースを独立したテストとして登録できる
		t.Run(tt.name, func(t *testing.T) {
			// repository層をモックに差し替えてserviceを初期化
			repo := &mockPostRepository{findByIDFunc: tt.repoFind}
			cache := &mockPostCache{getPostFunc: tt.cacheGet}
			svc := NewPostService(repo, cache)

			// テスト対象のメソッドを呼ぶ
			got, err := svc.GetPost(context.Background(), 1)

			// アサーション
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("GetPost() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && got.Body != tt.wantBody {
				t.Errorf("GetPost() body = %v, want %v", got.Body, tt.wantBody)
			}
			if repo.findByIDCalls != tt.wantRepoCalls {
				t.Errorf("FindByID calls = %v, want %v", repo.findByIDCalls, tt.wantRepoCalls)
			}
			if cache.setPostCalls != tt.wantSetCalls {
				t.Errorf("SetPost calls = %v, want %v", cache.setPostCalls, tt.wantSetCalls)
			}
		})
	}
}
