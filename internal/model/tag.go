package model

import "gorm.io/gorm"

type Tag struct {
	// gorm.Modelは、モデルに対する埋め込みstruct: https://gorm.io/docs/models.html#gorm-Model
	// 匿名fieldに他のstructを宣言すると、埋め込まれたstructのfiledがflattenされて、親modelのfieldになる
	gorm.Model
	// gormのMySQLドライバは通常、string型をlongtext型へ変換する
	// index/uniqueタグがあると、インデックスを貼るために、サイズ上限付きの型（varchar(191)）へ変換する
	Name  string  `gorm:"unique;not null"`
	Posts []*Post `gorm:"many2many:post_tags;"`
}
