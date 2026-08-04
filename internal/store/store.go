package store

import (
	"errors"
	"fmt"
	"sync"
)

type Document struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	Status string `json:"status"`
}

type DocumentStorage struct {
	documents map[int]Document
	mu        sync.RWMutex
	counter   int
}

func (ds *DocumentStorage) GetAll() []Document {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	dlist := make([]Document, 0, len(ds.documents))
	for _, d := range ds.documents {
		dlist = append(dlist, d)
	}
	return dlist
}

func (ds *DocumentStorage) Get(id int) (Document, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	doc, ok := ds.documents[id]
	if !ok {
		return Document{}, fmt.Errorf("document %v %w", id, ErrNotFound)
	}
	return doc, nil
}

func (ds *DocumentStorage) Add(doc Document) int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.counter++
	docId := ds.counter
	ds.documents[docId] = doc
	return docId
}

var (
	DefaultDocumentStorage = &DocumentStorage{
		documents: map[int]Document{
			1: {Author: "Tanaka", Body: "First one yeahhhh", Status: "public"},
			2: {Author: "Sato", Body: "Broken doc foo bar baz", Status: "private"},
			3: {Author: "Nagano", Body: "Can we do something? Yes we can!", Status: "public"},
		},
		mu:      sync.RWMutex{},
		counter: 3,
	}
	ErrNotFound = errors.New("not found")
)
