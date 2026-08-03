package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
)

type Document struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	Status string `json:"status"`
}

type DocStorage struct {
	documents map[int]Document
	mu        sync.RWMutex
	counter   int
}

func (ds *DocStorage) GetAll() []Document {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	dlist := make([]Document, 0, len(ds.documents))
	for _, d := range ds.documents {
		dlist = append(dlist, d)
	}
	return dlist
}

func (ds *DocStorage) Get(id int) (Document, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	doc, ok := ds.documents[id]
	if !ok {
		return Document{}, fmt.Errorf("document %v %w", id, ErrNotFound)
	}
	return doc, nil
}

func (ds *DocStorage) Add(doc Document) int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.counter++
	docId := ds.counter
	ds.documents[docId] = doc
	return docId
}

type IncompleteDocumentError struct {
	Fields []string
}

func (e *IncompleteDocumentError) Error() string {
	return fmt.Sprintf("document field cannot be empty: %v", e.Fields)
}

var (
	ErrNotFound = errors.New("not found")
	dStorage    = DocStorage{
		documents: map[int]Document{
			1: {Author: "Tanaka", Body: "First one yeahhhh", Status: "public"},
			2: {Author: "Sato", Body: "Broken doc foo bar baz", Status: "private"},
			3: {Author: "Nagano", Body: "Can we do something? Yes we can!", Status: "public"},
		},
		mu:      sync.RWMutex{},
		counter: 3,
	}
)

func healthzHandler() func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}

// グローバルなデータストアからDocument一覧を取得してJSONエンコードし、bodyとして返す
func getDocumentsHandler() func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		documentsJson, err := json.Marshal(dStorage.GetAll())
		if err != nil { // デコード失敗時は500を返す （ex. json.Marshal(make(chan int))）
			log.Printf("server error: document list is broken: %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(documentsJson)
	}
}

// グローバルなデータストアから指定されたidのDocumentを取得してJSONエンコードし、bodyとして返す
func getDocumentByIdHandler() func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		docId, err := strconv.Atoi(req.PathValue("id"))
		if err != nil {
			log.Printf("client error: %v", err)
			http.Error(w, "client error: invalid id", http.StatusBadRequest)
			return
		}
		doc, err := dStorage.Get(docId)
		if err != nil {
			switch {
			case errors.Is(err, ErrNotFound):
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
}

func validatePostedDoc(doc *Document) error {
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
	err := &IncompleteDocumentError{invalidFields}
	return fmt.Errorf("invalid posted document: %w", err)
}

// req bodyのJSON文字列をデコードし、新規Documentとしてグローバルなデータストアに追加する
func postDocumentHandler() func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		// req bodyのJSON文字列をデコード
		var doc Document
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
		docId := dStorage.Add(doc)
		w.Header().Set("Location", "/documents/"+strconv.Itoa(docId))
		w.WriteHeader(http.StatusCreated)
	}
}

func main() {

	// 任意のpathに対応するhandler関数を"DefaultServeMux"へ登録（ルーティング設定）
	// - DefaultServeMux: net/httpパッケージにあらかじめ用意されている、グローバルなマルチプレクサ（ServeMux）のインスタンス
	// - マルチプレクサ(Mux): N個の入力から、選択信号を元に、1個の出力を返す機構
	//   - 複数の入力(pattern, handler)から、選択信号(req)を元に、1個の出力（res）を返す機構
	http.HandleFunc("/healthz", healthzHandler())
	http.HandleFunc("GET /documents", getDocumentsHandler())
	http.HandleFunc("POST /documents", postDocumentHandler())
	http.HandleFunc("GET /documents/{id}", getDocumentByIdHandler())

	// http serverの起動。2nd argがnilなので、ルーティングにはDefaultServeMuxが使われる
	log.Fatal(http.ListenAndServe(":8080", nil))
}
