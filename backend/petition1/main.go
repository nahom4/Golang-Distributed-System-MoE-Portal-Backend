package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"petition1/config"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

// API DTOs (transport-layer structs)
type User struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

type TextDocument struct {
	Title   string `json:"title"`
	Text    string `json:"text"`
	OwnerId int    `json:"owner_id"`
}

type SignPetitionDTO struct {
    UserId       uint   `json:"UserId" binding:"required"`
    PetitionName string `json:"PetitionName" binding:"required"`
}

// In-memory live-edit cache (not the DB)
var (
	cacheMu sync.RWMutex
	cache   = make(map[string]TextDocument)
)

// WebSocket hub

type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	Send     chan *TextDocument
	Document TextDocument
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan *TextDocument
	register   chan *Client
	unregister chan *Client
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan *TextDocument),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}

		case doc := <-h.broadcast:
			cacheMu.Lock()
			cache[doc.Title] = *doc
			cacheMu.Unlock()

			for client := range h.clients {
				client.Document.Text = doc.Text
				select {
				case client.Send <- &client.Document:
				default:
					close(client.Send)
					delete(h.clients, client)
				}
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Data access helpers (GORM over in-memory SQLite)

func saveDocument(document TextDocument) (TextDocument, error) {
	if document.Title == "" {
		return document, errors.New("title must not be empty")
	}
	rec := database.Petition{
		Name:    document.Title,
		Text:    document.Text,
		OwnerId: document.OwnerId,
	}
	if err := database.DB.Create(&rec).Error; err != nil {
		return document, err
	}
	return document, nil
}

func getDocument(documentName string) (TextDocument, error) {
	var p database.Petition
	err := database.DB.
		Where("name = ?", documentName).
		Order("petition_id DESC").
		First(&p).Error
	if err != nil {
		return TextDocument{}, err
	}
	return TextDocument{
		Title:   p.Name,
		Text:    p.Text,
		OwnerId: p.OwnerId,
	}, nil
}

func getAll() ([]TextDocument, error) {
	// Subquery to get the latest PetitionID per Name
	sub := database.DB.Model(&database.Petition{}).
		Select("MAX(petition_id)").
		Group("name")

	var rows []database.Petition
	if err := database.DB.
		Where("petition_id IN (?)", sub).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]TextDocument, 0, len(rows))
	for _, r := range rows {
		out = append(out, TextDocument{
			Title:   r.Name,
			Text:    r.Text,
			OwnerId: r.OwnerId,
		})
	}
	return out, nil
}

func addSignature(petitionName string, userId uint) error {
	// Optionally ensure petition exists

	var count int64
	if err := database.DB.Model(&database.Petition{}).
		Where("name = ?", petitionName).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return errors.New("petition does not exist")
	}

	// Upsert-like behavior: composite primary key prevents duplicates
	sp := database.SignPetition{
		PetitionName: petitionName,
		UserId:       userId,
	}
	if err := database.DB.Create(&sp).Error; err != nil {
		// If duplicate, surface a friendly error
		return errors.New("user already signed this petition or insert failed")
	}
	return nil
}

func listSignatories(petitionName string) ([]User, error) {
	// Join users with sign_petitions on user_id
	var out []User
	err := database.DB.
		Table("users").
		Select("users.first_name, users.last_name, users.email").
		Joins("JOIN sign_petitions ON sign_petitions.user_id = users.user_id").
		Where("sign_petitions.petition_name = ?", petitionName).
		Scan(&out).Error
	
		
	return out, err
}

// WebSocket handlers

func handleConnections(hub *Hub, c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}

	documentName := c.Query("document")
	if documentName == "" {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("missing ?document=Title query parameter"))
		_ = conn.Close()
		return
	}

	// Load doc from cache or DB; if not found, start new empty doc
	var doc TextDocument
	cacheMu.RLock()
	cached, ok := cache[documentName]
	cacheMu.RUnlock()

	if ok {
		doc = cached
	} else {
		if existing, err := getDocument(documentName); err == nil {
			doc = existing
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			doc = TextDocument{Title: documentName, Text: "", OwnerId: 0}
		} else if err != nil {
			log.Println("getDocument error:", err)
			doc = TextDocument{Title: documentName, Text: "", OwnerId: 0}
		}
		cacheMu.Lock()
		cache[documentName] = doc
		cacheMu.Unlock()
	}

	client := &Client{hub: hub, conn: conn, Send: make(chan *TextDocument, 8), Document: doc}
	hub.register <- client

	go client.write()
	go client.read()
	client.Send <- &doc
}

func (c *Client) read() {
	defer func() {
		// When the last client disconnects, persist the latest version in cache
		if len(c.hub.clients) == 1 {
			cacheMu.RLock()
			latest := cache[c.Document.Title]
			cacheMu.RUnlock()
			if _, err := saveDocument(latest); err != nil {
				log.Println("save on last disconnect error:", err)
			}
		}
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		c.Document.Text = string(message)
		c.hub.broadcast <- &c.Document
	}
}

func (c *Client) write() {
	defer c.conn.Close()
	for {
		select {
		case doc, ok := <-c.Send:
			if !ok {
				return
			}
			c.Document.Text = doc.Text
			if err := c.conn.WriteMessage(websocket.TextMessage, []byte(doc.Text)); err != nil {
				return
			}
		}
	}
}

// HTTP handlers

func getAllPetitions(ctx *gin.Context) {
	petitions, err := getAll()
	if err != nil {
		ctx.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve petitions"})
		return
	}
	ctx.IndentedJSON(http.StatusOK, petitions)
}

func createPetition(ctx *gin.Context) {
	var document TextDocument
	if err := ctx.BindJSON(&document); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if document.Title == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Title must not be empty"})
		return
	}

	// If exists, conflict
	if _, err := getDocument(document.Title); err == nil {
		ctx.JSON(http.StatusConflict, gin.H{"error": "Petition already exists"})
		return
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check petition existence"})
		return
	}

	doc, err := saveDocument(document)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create petition"})
		return
	}

	cacheMu.Lock()
	cache[doc.Title] = doc
	cacheMu.Unlock()

	ctx.JSON(http.StatusOK, gin.H{"message": "Petition created successfully", "petition": doc})
}

func signPetition(ctx *gin.Context) {
	var sp SignPetitionDTO
	if err := ctx.BindJSON(&sp); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if sp.PetitionName == "" || sp.UserId == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "PetitionName and UserId are required"})
		return
	}

	if err := addSignature(sp.PetitionName, sp.UserId); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Petition signed successfully", "signPetition": sp})
}

func getSignatories(ctx *gin.Context) {
	petitionName := ctx.Query("PetitionName") 
	if petitionName == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "PetitionName is required"})
		return
	}

	users, err := listSignatories(petitionName)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve signatories"})
		return
	}
	ctx.IndentedJSON(http.StatusOK, users)
}

// Periodic save of live-edit cache into the DB
func periodicSave(cache map[string]TextDocument) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		cacheMu.RLock()
		snapshot := make([]TextDocument, 0, len(cache))
		for _, value := range cache {
			snapshot = append(snapshot, value)
		}
		cacheMu.RUnlock()

		for _, value := range snapshot {
			if _, err := saveDocument(value); err != nil {
				fmt.Printf("Error saving document %s: %v\n", value.Title, err)
			}
		}
	}
}

func main() {
	// Initialize in-memory DB
	database.ConnectDB()

	// Start WebSocket hub and periodic saver
	hub := newHub()
	go hub.run()
	go periodicSave(cache)

	// Router
	router := gin.Default()
	router.GET("/ws", func(c *gin.Context) {
		handleConnections(hub, c)
	})
	router.GET("/petitions", getAllPetitions)
	router.POST("/createPetition", createPetition)
	router.POST("/signPetition", signPetition)
	router.GET("/signatories", getSignatories)

	log.Println("Server is running on :3032")
	if err := router.Run("localhost:3032"); err != nil {
		log.Fatal(err)
	}
}