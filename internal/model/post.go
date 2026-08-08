package model

import "gorm.io/gorm"

type Post struct {
	// gorm.Modelは、モデルに対する埋め込みstruct: https://gorm.io/docs/models.html#gorm-Model
	// 匿名fieldに他のstructを宣言すると、埋め込まれたstructのfiledがflattenされて、親modelのfieldになる
	gorm.Model
	Body string `gorm:"not null"`
	// gormが<所有者の型名>+<所有者のPK名>を探し、見つかったfieldをFKとして採用する（FK制約の適用はAutoMigrate()が行う）
	UserID uint   `gorm:"not null"`
	Tags   []*Tag `gorm:"many2many:post_tags;"`
}
