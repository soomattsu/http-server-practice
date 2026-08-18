// handler はhttp req/resを処理する関数を提供する。
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/soomattsu/http-server-practice/internal/document/repository"
)

// IncompleteDocumentError はいずれかのfieldが空のDocumentがPOSTされた時にPostDocumentHandlerから返される。
type IncompleteDocumentError struct {
	Fields []string
}

func (e *IncompleteDocumentError) Error() string {
	return fmt.Sprintf("document field cannot be empty: %v", e.Fields)
}

func RegisterDocument(mux *http.ServeMux) *http.ServeMux {
	// 任意のpathに対応するhandler関数を"ServeMux"へ登録（ルーティング設定）
	// - DefaultServeMux: net/httpパッケージにあらかじめ用意されている、グローバルなマルチプレクサ（ServeMux）のインスタンス
	// - マルチプレクサ(Mux): N個の入力から、選択信号を元に、1個の出力を返す機構
	//   - 複数の入力(pattern, handler)から、選択信号(req)を元に、1個の出力（res）を返す機構
	// - ここでは受け取ったServeMuxへ登録する
	mux.HandleFunc("GET /documents", GetDocuments)
	mux.HandleFunc("POST /documents", PostDocument)
	mux.HandleFunc("GET /documents/{id}", GetDocumentByID)
	return mux
}

// GetDocuments はグローバルなデータストアからDocument一覧を取得してJSONエンコードし、bodyとして返す。
func GetDocuments(w http.ResponseWriter, _ *http.Request) {
	docJSON, err := json.Marshal(repository.DefaultDocuments.GetAll())
	if err != nil { // エンコード失敗時は500を返す （ex. json.Marshal(make(chan int))）
		log.Printf("Server error: document list is broken: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(docJSON)
}

// GetDocumentByID はグローバルなデータストアから指定されたidのDocumentを取得してJSONエンコードし、bodyとして返す。
func GetDocumentByID(w http.ResponseWriter, req *http.Request) {
	docID, err := strconv.Atoi(req.PathValue("id"))
	if err != nil {
		log.Printf("Client error: %v", err)
		http.Error(w, "Client error: invalid id", http.StatusBadRequest)
		return
	}
	doc, err := repository.DefaultDocuments.Get(docID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			log.Printf("Client error: %v", err)
			http.Error(w, fmt.Sprintf("Client error: document[%v] not found", docID), http.StatusNotFound)
		default:
			log.Printf("Server error: %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		return
	}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		log.Printf("Server error: document[%v] broken: %v", docID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(docJSON)
}

// PostDocument はreq bodyのJSON文字列をデコードし、新規Documentとしてグローバルなデータストアに追加する。
func PostDocument(w http.ResponseWriter, req *http.Request) {
	// req bodyのJSON文字列をデコード
	const maxBytes = 1024
	body := http.MaxBytesReader(w, req.Body, maxBytes)
	var doc repository.Document
	if err := json.NewDecoder(body).Decode(&doc); err != nil {
		var expErr *http.MaxBytesError
		switch {
		case errors.As(err, &expErr):
			log.Printf("Client error: sent too large json string(limit %v bytes)", expErr.Limit)
			http.Error(w, "Client error: sent too large document data", http.StatusRequestEntityTooLarge)
		default:
			log.Printf("Client error: sent invalid json string: %v", err)
			http.Error(w, "Client error: sent invalid document data", http.StatusBadRequest)
		}
		return
	}
	if err := validatePostedDoc(&doc); err != nil {
		var expErr *IncompleteDocumentError
		switch {
		case errors.As(err, &expErr):
			http.Error(w, fmt.Sprintf("Client error: fill out %v", expErr.Fields), http.StatusBadRequest)
		default:
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		}
		log.Printf("Client error: %v", err)
		return
	}
	// 採番して格納
	docID := repository.DefaultDocuments.Add(doc)
	w.Header().Set("Location", "/documents/"+strconv.Itoa(docID))
	w.WriteHeader(http.StatusCreated)
}

func validatePostedDoc(doc *repository.Document) error {
	fields := make([]string, 0, 3)
	if doc.Author == "" {
		fields = append(fields, "Author")
	}
	if doc.Body == "" {
		fields = append(fields, "Body")
	}
	if doc.Status == "" {
		fields = append(fields, "Status")
	}
	if len(fields) == 0 {
		return nil
	}
	err := &IncompleteDocumentError{Fields: fields}
	return fmt.Errorf("invalid posted document: %w", err)
}
