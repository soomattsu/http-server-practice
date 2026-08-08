package model

import "errors"

var (
	ErrPostNotFound         = errors.New("post not found")
	ErrPostHasInvalidUserID = errors.New("post has invalid userID")
	ErrInvalidPostInput     = errors.New("body and userID cannot be empty")
)
