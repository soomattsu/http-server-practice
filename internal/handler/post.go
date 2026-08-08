package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/soomattsu/http-server-practice/internal/model"
	"github.com/soomattsu/http-server-practice/internal/service"
)

type PostHandler struct {
	// serviceの差し替えは現状想定されていないので、interfaceによる抽象化は不要
	svc *service.PostService
}

func NewPostHandler(svc *service.PostService) *PostHandler {
	return &PostHandler{svc: svc}
}

func (h *PostHandler) Register(mux *http.ServeMux) *http.ServeMux {
	mux.HandleFunc("GET /posts", h.ListPost)
	mux.HandleFunc("GET /posts/{id}", h.GetPost)
	mux.HandleFunc("POST /posts", h.CreatePost)
	mux.HandleFunc("PATCH /posts/{id}", h.UpdatePost)
	mux.HandleFunc("DELETE /posts/{id}", h.DeletePost)
	return mux
}

type postsOutput struct {
	ID        uint      `json:"id"`
	UserID    uint      `json:"userId"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (h *PostHandler) ListPost(w http.ResponseWriter, r *http.Request) {
	posts, err := h.svc.ListPost()
	if err != nil {
		log.Printf("Server error: failed to list Post %v", err)
		http.Error(w, "Server error: failed to list all Post", http.StatusInternalServerError)
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

	// JSON文字列へエンコード
	outJSON, err := json.Marshal(out)
	if err != nil {
		log.Printf("Server error: Post list is broken: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(outJSON)
}

func (h *PostHandler) GetPost(w http.ResponseWriter, r *http.Request) {
	id, ok := validateID(w, r)
	if !ok {
		return
	}

	post, err := h.svc.GetPost(id)
	if err != nil {
		if errors.Is(err, model.ErrPostNotFound) {
			log.Printf("Client error: Post not found with ID[%v]: %v", id, err)
			http.Error(w, "Client error: no such Post", http.StatusNotFound)
			return
		}
		log.Printf("Server error: failed to get Post with ID[%v]: %v", id, err)
		http.Error(w, "Server error: failed to get Post", http.StatusInternalServerError)
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

	// JSON文字列へエンコード
	outJSON, err := json.Marshal(out)
	if err != nil {
		log.Printf("Server error: post is broken: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(outJSON)
}

type createPostInput struct {
	UserID uint   `json:"userId"`
	Body   string `json:"body"`
}

func (h *PostHandler) CreatePost(w http.ResponseWriter, r *http.Request) {
	// 入力用structへの変換（無効fieldが含まれているreqを弾く）
	var in createPostInput
	err := initDecoder(w, r).Decode(&in)
	if err != nil {
		log.Printf("Client error: sent invalid request: %v", err)
		http.Error(w, "Client error: sent invalid reqeust", http.StatusBadRequest)
		return
	}

	postID, err := h.svc.CreatePost(model.Post{UserID: in.UserID, Body: in.Body})
	if err != nil {
		if errors.Is(err, model.ErrInvalidPostInput) {
			log.Printf("Client error: sent invalid Post: %v", err)
			http.Error(w, "Client error: sent invalid Post: field cannot be empty", http.StatusBadRequest)
			return
		}
		if errors.Is(err, model.ErrPostHasInvalidUserID) {
			log.Printf("Client error: sent invalid Post: %v", err)
			http.Error(w, "Client error: sent invalid Post: no such userId", http.StatusBadRequest)
			return
		}
		log.Printf("Server error: failed to create Post: %v", err)
		http.Error(w, "Server error: failed to create Post", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Location", "/posts/"+strconv.Itoa(int(postID)))
	w.WriteHeader(http.StatusCreated)
}

type updatePostInput struct {
	Body string `json:"body"`
}

func (h *PostHandler) UpdatePost(w http.ResponseWriter, r *http.Request) {
	id, ok := validateID(w, r)
	if !ok {
		return
	}
	// 入力用structへの変換（無効fieldが含まれているreqを弾く）
	var in updatePostInput
	err := initDecoder(w, r).Decode(&in)
	if err != nil {
		log.Printf("Client error: sent invalid request: %v", err)
		http.Error(w, "Client error: sent invalid request", http.StatusBadRequest)
		return
	}

	if err := h.svc.UpdatePost(id, in.Body); err != nil {
		if errors.Is(err, model.ErrInvalidPostInput) {
			log.Printf("Client error: sent invalid request: body is empry: %v", err)
			http.Error(w, "Client error: sent invalid request: new body required", http.StatusBadRequest)
			return
		}
		if errors.Is(err, model.ErrPostNotFound) {
			log.Printf("Client error: Post to UPDATE not found with ID[%v]: %v", id, err)
			http.Error(w, "Client error: no such Post to update", http.StatusNotFound)
			return
		}
		log.Printf("Server error: failed to UPDATE Post with ID[%v]: %v", id, err)
		http.Error(w, "Server error: failed to update Post", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *PostHandler) DeletePost(w http.ResponseWriter, r *http.Request) {
	id, ok := validateID(w, r)
	if !ok {
		return
	}

	if err := h.svc.DeletePost(id); err != nil {
		log.Printf("Server error: failed to DELETE Post with ID[%v]: %v", id, err)
		http.Error(w, "Server error: failed to delete Post", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// validateID はpath parameter: idが正の整数であることを検証する。
func validateID(w http.ResponseWriter, r *http.Request) (uint, bool) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 0)
	if err != nil {
		log.Printf("Client error: specified invalid id: %v", err)
		http.Error(w, "Client error: invalid Post ID", http.StatusBadRequest)
		return 0, false
	}
	// クエリ実行時、placeholderでwhere句を構成するなら、文字列をそのまま渡してもSQLインジェクションは起きない
	// MySQL側の暗黙変換による予期しない結果を予防し、モデル側型定義に合わせるためにuintへキャストする
	return uint(id), true
}

// initDecoder は以下のJSON文字列を弾くDecoderを返す。
// サイズ >= 1KB || Decode先のstructとして無効なフィールドが存在する。
func initDecoder(w http.ResponseWriter, r *http.Request) *json.Decoder {
	const maxBytes = 1024
	body := http.MaxBytesReader(w, r.Body, maxBytes)
	d := json.NewDecoder(body)
	d.DisallowUnknownFields()
	return d
}
