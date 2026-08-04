package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/soomattsu/http-server-practice/internal/store"
)

type IncompleteDocumentError struct {
	Fields []string
}

func (e *IncompleteDocumentError) Error() string {
	return fmt.Sprintf("document field cannot be empty: %v", e.Fields)
}

func HealthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// グローバルなデータストアからDocument一覧を取得してJSONエンコードし、bodyとして返す
func GetDocumentsHandler(w http.ResponseWriter, _ *http.Request) {
	documentsJson, err := json.Marshal(store.DefaultDocumentStorage.GetAll())
	if err != nil { // デコード失敗時は500を返す （ex. json.Marshal(make(chan int))）
		log.Printf("server error: document list is broken: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(documentsJson)
}

// グローバルなデータストアから指定されたidのDocumentを取得してJSONエンコードし、bodyとして返す
func GetDocumentByIdHandler(w http.ResponseWriter, req *http.Request) {
	docId, err := strconv.Atoi(req.PathValue("id"))
	if err != nil {
		log.Printf("client error: %v", err)
		http.Error(w, "client error: invalid id", http.StatusBadRequest)
		return
	}
	doc, err := store.DefaultDocumentStorage.Get(docId)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			log.Printf("client error: %v", err)
			http.Error(w, fmt.Sprintf("client error: document[%v] not found", docId), http.StatusNotFound)
		default:
			log.Printf("server error: %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		return
	}
	docJson, err := json.Marshal(doc)
	if err != nil {
		log.Printf("server error: document[%v] broken: %v", docId, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(docJson)
}

// req bodyのJSON文字列をデコードし、新規Documentとしてグローバルなデータストアに追加する
func PostDocumentHandler(w http.ResponseWriter, req *http.Request) {
	// req bodyのJSON文字列をデコード
	var doc store.Document
	if err := json.NewDecoder(req.Body).Decode(&doc); err != nil {
		log.Printf("client error: sent invalid json string: %v", err)
		http.Error(w, "client error: sent invalid document data", http.StatusBadRequest)
		return
	}
	if err := validatePostedDoc(&doc); err != nil {
		var expectedErr *IncompleteDocumentError
		switch {
		case errors.As(err, &expectedErr):
			http.Error(w, fmt.Sprintf("client error: fill out %v", expectedErr.Fields), http.StatusBadRequest)
		default:
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		}
		log.Printf("client error: %v", err)
		return
	}
	// 採番して格納
	docId := store.DefaultDocumentStorage.Add(doc)
	w.Header().Set("Location", "/documents/"+strconv.Itoa(docId))
	w.WriteHeader(http.StatusCreated)
}

func validatePostedDoc(doc *store.Document) error {
	invalidFields := make([]string, 0, 3)
	if doc.Author == "" {
		invalidFields = append(invalidFields, "Author")
	}
	if doc.Body == "" {
		invalidFields = append(invalidFields, "Body")
	}
	if doc.Status == "" {
		invalidFields = append(invalidFields, "Status")
	}
	if len(invalidFields) == 0 {
		return nil
	}
	err := &IncompleteDocumentError{Fields: invalidFields}
	return fmt.Errorf("invalid posted document: %w", err)
}
