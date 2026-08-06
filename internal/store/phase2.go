package store

import (
	"fmt"
	"log"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	DB *gorm.DB
)

func InitMySQL(cfg *Config) error {
	dsn := fmt.Sprintf(
		"%v:%v@tcp(localhost:%v)/%v?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.MySQLUser,
		cfg.MySQLPassword,
		cfg.MySQLPort,
		cfg.MySQLDatabase,
	)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		TranslateError: true,
		Logger:         logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return fmt.Errorf("failed to open: %v", err)
	}
	DB = db
	if err := DB.AutoMigrate(&User{}, &Post{}, &Tag{}); err != nil {
		return fmt.Errorf("failed to migrate: %v", err)
	}
	log.Println("Success to init table schema on MySQL!")
	return nil
}

type User struct {
	gorm.Model
	Name  string `gorm:"not null"`
	Age   *uint8
	Posts []Post
}

type Post struct {
	gorm.Model
	Body   string `gorm:"not null"`
	UserID uint   `gorm:"not null"`
	Tags   []*Tag `gorm:"many2many:post_tags;"`
}

type Tag struct {
	gorm.Model
	Name  string  `gorm:"unique;not null"`
	Posts []*Post `gorm:"many2many:post_tags;"`
}
