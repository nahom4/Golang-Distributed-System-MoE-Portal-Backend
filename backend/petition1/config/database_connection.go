package database

import (
	"log"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// If you want a pure Go SQLite driver (no CGO), replace the import above with:
//   "github.com/glebarez/sqlite"
// and change sqlite.Open(...) to sqlite.Open(...)

// DB is the global GORM database handle.
var DB *gorm.DB

// Models

type Petition struct {
	PetitionID uint   `gorm:"primaryKey;autoIncrement"`
	Name       string `gorm:"index;not null"`
	Text       string `gorm:"type:text"`
	OwnerId    int
	CreatedAt  time.Time
}

type User struct {
	UserID    uint   `gorm:"primaryKey;autoIncrement"`
	FirstName string `gorm:"type:text"`
	LastName  string `gorm:"type:text"`
	Email     string `gorm:"type:text"`
	Password  string `gorm:"type:text"`
}

type SignPetition struct {
	PetitionName string `gorm:"primaryKey;size:256"`
	UserId       uint   `gorm:"primaryKey"`
}

// ConnectDB initializes an in-memory SQLite database and runs migrations.
func ConnectDB() {
	// Use a named shared in-memory DB and hold a single open connection.
	// This prevents losing the DB when pooling opens/closes new conns.
	// dsn := "file:memdb1?mode=memory&cache=shared"

	db, err := gorm.Open(sqlite.Open("../../shared.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to in-memory DB: ", err)
	}

	// Limit to a single open conn so the shared in-memory DB persists reliably.
	// sqlDB, err := db.DB()
	if err != nil {
		log.Fatal("Failed to access underlying sql.DB: ", err)
	}
	// sqlDB.SetMaxOpenConns(1)

	// Run migrations
	if err := db.AutoMigrate(&Petition{}, &SignPetition{}); err != nil {
		log.Fatal("Failed to migrate schemas: ", err)
	}

	DB = db
}