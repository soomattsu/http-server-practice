package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/soomattsu/http-server-practice/internal/document/handler"
)

func main() {
	router := handler.NewRouter()

	s := &http.Server{
		Addr:              ":8080",
		Handler:           router,
		ReadHeaderTimeout: 1 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := s.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-sigCtx.Done():
		log.Println("Graceful shutdown: started")
		sdCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := s.Shutdown(sdCtx); err != nil {
			log.Printf("Graceful shutdown: error: %v", err)
		}
	case err := <-errCh:
		log.Fatalf("HTTP server ListenAndServe failed: %v", err)
	}
}
