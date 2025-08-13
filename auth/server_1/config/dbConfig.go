package database

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"log"
)

// Declare DB as a global variable
var DB *gorm.DB

func ConnectDB() {
  db, err := gorm.Open(sqlite.Open("../../shared.db"), &gorm.Config{})
  if err != nil {
    log.Fatal("Failed to connect to in-memory DB", err)
  }
  DB = db
}
