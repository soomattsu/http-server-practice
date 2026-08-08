package repository

import (
	"errors"
	"fmt"
	"sync"
)

var (
	// DefaultDocuments は、シードデータを持つデフォルトのDocumentsの値で、メモリ上に保持される。
	DefaultDocuments = &Documents{
		documents: map[int]Document{
			1: {Author: "Tanaka", Body: "First one yeahhhh", Status: "public"},
			2: {Author: "Sato", Body: "Broken doc foo bar baz", Status: "private"},
			3: {Author: "Nagano", Body: "Can we do something? Yes we can!", Status: "public"},
		},
		mu:      sync.RWMutex{},
		counter: 3,
	}
	// ErrNotFound は、指定されたidのDocumentが存在しなかった場合にGetからwrapして返される。
	ErrNotFound = errors.New("not found")
)

// Document はこのハンズオンで利用するデータモデルを表す。
type Document struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	Status string `json:"status"`
}

// Documents はDocumentを格納する構造体。
type Documents struct {
	documents map[int]Document
	mu        sync.RWMutex
	counter   int
}

// GetAll はDocumentsが持つDocumentをすべて返す。
func (ds *Documents) GetAll() []Document {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	dlist := make([]Document, 0, len(ds.documents))
	for _, d := range ds.documents {
		dlist = append(dlist, d)
	}
	return dlist
}

// Get はDocumentsからidに対応するDocumentを1つ返す。
func (ds *Documents) Get(id int) (Document, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	doc, ok := ds.documents[id]
	if !ok {
		return Document{}, fmt.Errorf("document %v %w", id, ErrNotFound)
	}
	return doc, nil
}

// Add はDocumentsに新しいDocumentを追加してidを付与する。
func (ds *Documents) Add(doc Document) int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.counter++
	docID := ds.counter
	ds.documents[docID] = doc
	return docID
}
