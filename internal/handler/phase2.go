package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/soomattsu/http-server-practice/internal/store"
	"gorm.io/gorm"
)

type createPostInput struct {
	UserID uint   `json:"userId"`
	Body   string `json:"body"`
}

type postsOutput struct {
	ID        uint      `json:"id"`
	UserID    uint      `json:"userId"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func initDecoder(w http.ResponseWriter, r *http.Request) *json.Decoder {
	const maxBytes = 1024
	body := http.MaxBytesReader(w, r.Body, maxBytes)
	d := json.NewDecoder(body)
	d.DisallowUnknownFields()
	return d
}

func GetPosts(w http.ResponseWriter, r *http.Request) {
	// SELECT * from postsして、postsへ結果を書き込む
	var posts []store.Post
	if err := store.DB.Find(&posts).Error; err != nil {
		log.Printf("Server error: failed to get Posts: %v", err)
		http.Error(w, "Server error: failed to get all Post data", http.StatusInternalServerError)
		return
	}
	// 出力用structへ変換
	out := make([]postsOutput, len(posts))
	for i := range len(posts) {
		out[i] = postsOutput{
			ID:        posts[i].ID,
			UserID:    posts[i].UserID,
			Body:      posts[i].Body,
			CreatedAt: posts[i].CreatedAt,
			UpdatedAt: posts[i].UpdatedAt,
		}
	}
	// response用にJSON文字列へエンコード
	outJSON, err := json.Marshal(out)
	if err != nil {
		log.Printf("Server error: post list is broken: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(outJSON)
}

func GetPostByID(w http.ResponseWriter, r *http.Request) {
	// クエリ実行時、placeholderでwhere句を構成するなら、PathValue文字列をそのまま渡してもSQLインジェクションは起きない
	// ここでは、MySQL側の暗黙変換による予期しない結果を予防するためにintへキャストする
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		log.Printf("Client error: specified invalid id: %v", err)
		http.Error(w, "Client error: invalid Post ID", http.StatusBadRequest)
		return
	}

	var post store.Post
	// SELECT * from posts WHERE id = {id}して、postsへ結果を書き込む
	if err := store.DB.First(&post, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("Client error: Post not found with ID[%v]: %v", id, err)
			http.Error(w, "Client error: no such Post", http.StatusNotFound)
			return
		}
		log.Printf("Server error: failed to retrieve Post with ID[%v]: %v", id, err)
		http.Error(w, "Server error: failed to find Post data", http.StatusInternalServerError)
		return
	}
	// 出力用structへ変換
	out := postsOutput{
		ID:        post.ID,
		UserID:    post.UserID,
		Body:      post.Body,
		CreatedAt: post.CreatedAt,
		UpdatedAt: post.UpdatedAt,
	}
	// response用JSON文字列へエンコード
	outJSON, err := json.Marshal(out)
	if err != nil {
		log.Printf("Server error: post is broken: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(outJSON)
}

func CreatePost(w http.ResponseWriter, r *http.Request) {
	// クライアントが可能なfieldの限定と、ゼロ値や無効fieldを弾くvalidation
	var in createPostInput
	err := initDecoder(w, r).Decode(&in)
	if err != nil || in.Body == "" || in.UserID == 0 {
		log.Printf("Client error: sent invalid Post: %+v (decode err: %v)", in, err)
		http.Error(w, "Client error: sent invalid Post data", http.StatusBadRequest)
		return
	}

	post := store.Post{UserID: in.UserID, Body: in.Body}
	// INSERTして、作成したレコードに紐づくモデルをpostへ書き込む
	if err := store.DB.Create(&post).Error; err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			log.Printf("Client error: sent invalid Post: %v", err)
			http.Error(w, "Client error: sent invalid Post data, no such userId", http.StatusBadRequest)
			return
		}
		log.Printf("Server error: failed to insert Post: %v", err)
		http.Error(w, "Server error: failed to create Post data", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Location", "/posts/"+strconv.Itoa(int(post.ID)))
	w.WriteHeader(http.StatusCreated)
}

func UpdatePost(w http.ResponseWriter, r *http.Request) {
	http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
}

func DeletePost(w http.ResponseWriter, r *http.Request) {
	http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
}
