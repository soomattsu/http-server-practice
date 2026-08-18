package repository

import (
	"fmt"
	"log"

	"github.com/soomattsu/http-server-practice/internal/model"
	"gorm.io/gorm"
)

// Seed はスキーママイングレーションを行い、ダミーデータを投入する。
// 起動のたびに走るので、すべて FirstOrCreate / Replace で冪等にしてある。
func Seed(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.User{}, &model.Post{}, &model.Tag{}); err != nil {
		return fmt.Errorf("failed to migrate: %v", err)
	}
	log.Println("Success to migrate table schema!")

	users := []model.User{{Name: "alice"}, {Name: "bob"}}
	for i := range users {
		// Where のstruct条件はゼロ値fieldを無視する。Name で引いて、無ければその値で INSERT
		if err := db.Where(model.User{Name: users[i].Name}).FirstOrCreate(&users[i]).Error; err != nil {
			return fmt.Errorf("failed to seed users: %v", err)
		}
	}

	tags := []model.Tag{{Name: "go"}, {Name: "mysql"}, {Name: "gorm"}}
	for i := range tags {
		if err := db.Where(model.Tag{Name: tags[i].Name}).FirstOrCreate(&tags[i]).Error; err != nil {
			return fmt.Errorf("failed to seed tags: %v", err)
		}
	}

	posts := []model.Post{
		{Body: "alice's 1st post", UserID: users[0].ID},
		{Body: "alice's 2nd post", UserID: users[0].ID},
		{Body: "bob's 1st post", UserID: users[1].ID},
	}
	for i := range posts {
		if err := db.Where(model.Post{Body: posts[i].Body, UserID: posts[i].UserID}).FirstOrCreate(&posts[i]).Error; err != nil {
			return fmt.Errorf("failed to seed posts: %v", err)
		}
	}

	// N:N の関連付け。
	// Append ではなく Replace なのは「この集合にする」という意味なので、再実行しても post_tags に重複行が積まれないため
	postTags := [][]*model.Tag{
		{&tags[0], &tags[1]}, // alice's 1st -> go, mysql
		{&tags[0]},           // alice's 2nd -> go
		{&tags[2]},           // bob's 1st   -> gorm
	}
	for i := range posts {
		// 1: postsテーブルの何行目のレコードへの操作なのかを、r.db.Model(&posts[i])で指定
		// 2: 1のレコードにおけるどのrelationへの操作なのかを、Association("Tags")で指定
		// 3: 2で指定したrelationを、Replace(postTags[i])で置き換え
		// つまり、i番目のpostsレコードに、postTagsのi番目の要素（複数のtagsレコード）を紐づけている
		// Associationの副作用として、postsレコードのupdated_atが毎回（Post.Tagsに変化がなかった場合も含む）更新される
		if err := db.Model(&posts[i]).Association("Tags").Replace(postTags[i]); err != nil { // Association().Replace() は error を返す（.Error は不要）
			return fmt.Errorf("failed to seed post_tags: %v", err)
		}
	}

	log.Println("Success to seed dummy records!")
	return nil
}

func CheckPreloadBehavior(db *gorm.DB) {
	// gormでは、DBからマッピングしたモデルのrelation field（ex. user.Posts）はデフォルトで空になるので、暗黙的lazy Loadingは行わない
	// - 暗黙的lazy Loading: モデルが持つrelationを、そのfieldが参照されたタイミングで取得する（ex. Rails:ActiveRecord, Java:Hibernate）
	//   - 「userをN件取得->N回loopしてuser.Postsを参照」を行うと、loop内で毎回クエリが走り、N+1問題が発生！
	// - eager Loading: 取得対象のモデルが持つrelationも、最初にまとめて取得する
	// gormが提供するのは、eager（Preload）と明示的lazy（Association().Find）だけ

	// 1:N, Preload無し：relation fieldを参照すると空になっている
	var users1 []model.User
	db.Find(&users1)
	log.Printf("User <%v>'s Posts <%v> should be empty", users1[0].Name, users1[0].Posts)

	// 1:N, Preloadあり：relation fieldに値が入っている
	// 1. 親を全件取得 -> SELECT * from users
	// 2. 1の結果から、PKのリスト・{PK:モデル}のmapを作る -> [1,2,...]・{ 1: users[0], 2: users[1],...}
	// 3. 2のPKリストで絞り込んで、子を取得 -> SELECT * FROM posts WHERE posts.user_id IN (1,2,...)
	// 4. 子をloopして、<子>.<親のPK>を使って2のmapを参照し、マッチした親の子フィールドへappend
	// DB I/Oは2回で完了するのでN+1を回避できる（N:N relationでは中間テーブル参照が増えるのでDB I/Oは3回）
	var users2 []model.User
	db.Preload("Posts").Find(&users2)
	log.Printf("User <%v>'s Posts <%v> should be fulfilled", users2[0].Name, users2[0].Posts[0].Body)

	// N:N, Preloadあり
	var posts1 []model.Post
	db.Preload("Tags").Find(&posts1)
	log.Printf("Post <%v>'s Tags <%v> should be fulfilled", posts1[0].Body, posts1[0].Tags[0].Name)
}
