package controller

import (
	database "authServer1/config"
	models "authServer1/model"
	"errors"
	"time"

	"github.com/dgrijalva/jwt-go"
	"golang.org/x/crypto/bcrypt"
)
var jwtKey = []byte("secret_key")
type User struct {
	Username string
	Password string
}
type NewUser struct {
	User
	Email string
	Role string `json:"role"`

}
type Claims struct {
	Username string `json:"username"`
	UserID   uint   `json:"user_id"`
	Role string `json:"role"`
	jwt.StandardClaims
}

func RegisterUser(credentials NewUser) bool {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(credentials.Password), bcrypt.DefaultCost)
	if err != nil {
		return false
	}
	result := database.DB.Create(
		&models.User{
			Username: credentials.Username,
			Password: string(hashedPassword),
			Email: credentials.Email,
			Role: credentials.Role,
		},
	)


	return result.Error == nil
}

type LoginResult struct {
	Token string
	ExpireDate time.Time
}
func Login(credentials User) (LoginResult, error) {
	expectedPassword, err := getUserPassword(credentials.Username)
	if err != nil {
		return LoginResult{}, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(expectedPassword), []byte(credentials.Password))
	if err != nil {
		return LoginResult{}, err
	}

	expirationTime := time.Now().Add(time.Hour * 500)
	userID, err := getUserID(credentials.Username)
    if err != nil  {
		return LoginResult{}, err
    }

	userRole, err := getUserRole(credentials.Username)
    if err != nil  {
        return LoginResult{}, err
    }
	claims := &Claims{
		Username: credentials.Username,
		Role:  userRole,
		UserID:   userID,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtKey)

	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{
		Token:      tokenString,
		ExpireDate: expirationTime,
	}, nil
}

func ValidateToken(token string) bool {
	claims := jwt.MapClaims{}
	tkn, err := jwt.ParseWithClaims(token, claims,
		func(t *jwt.Token) (interface{}, error) {
			return jwtKey, nil
		})
	
	if err != nil {
		if err == jwt.ErrSignatureInvalid {
			return false
		}
		return false
	}

	if !tkn.Valid {
		return false
	}

	return true
}

func Refresh(tokenStr string)  (LoginResult, error) {
	claims := &Claims{}
	tkn, err := jwt.ParseWithClaims(tokenStr, claims,
		func(t *jwt.Token) (interface{}, error) {
			return jwtKey, nil
		})

	if err != nil {
		if err == jwt.ErrSignatureInvalid {
			return LoginResult{}, err
		}
		return LoginResult{}, err
	}
	if !tkn.Valid {
		return LoginResult{}, err
	}

	expirationTime := time.Now().Add(time.Hour * 500)

	claims.ExpiresAt = expirationTime.Unix()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtKey)

	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		Token:      tokenString,
		ExpireDate: expirationTime,
	}, nil

}
func getUserPassword(username string) (string, error) {
	var user models.User
	database.DB.Where("username = ?", username).First(&user)
	if user.UserID == 0 {
		return "", nil
	}
	return user.Password, nil
}

func getUserID(username string) (uint, error) {
    var user models.User
    database.DB.Where("username = ?", username).First(&user)
    if user.UserID == 0 {
        return 0, errors.New("User not found")
    }
    return user.UserID, nil
}
func getUserRole(username string) (string, error) {
    var user models.User
    database.DB.Where("username = ?", username).First(&user)
    if user.UserID == 0 {
        return "", errors.New("User not found")
    }
    return user.Role, nil
}