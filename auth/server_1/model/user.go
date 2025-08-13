package models

// import "gorm.io/gorm"

type User struct {
    // gorm.Model           // Provides ID as primary key
    UserID    uint `gorm:"autoIncrement"` // just auto-increments, not primary
    Username  string
    Password  string
    Email     string
    FirstName string
    LastName  string
    Role      string `gorm:"default:student"`
}