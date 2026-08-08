package model

import "gorm.io/gorm"

type User struct {
	// gorm.Modelは、モデルに対する埋め込みstruct: https://gorm.io/docs/models.html#gorm-Model
	// 匿名fieldに他のstructを宣言すると、埋め込まれたstructのfiledがflattenされて、親modelのfieldになる
	gorm.Model
	Name string `gorm:"not null"`
	Age  *uint8
	// fieldにstructがあると、gormは原則それをrelationとして解釈する
	// （例外：埋め込み・タグでの無視・Scanner/Valuerを実装した型）
	Posts []Post
}
