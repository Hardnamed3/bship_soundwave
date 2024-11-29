package main

import (
	"database/sql"
	"fmt"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"log"
	"net/http"
)

type Message struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
	UserID  int    `json:"user_id"`
	//need username to be displayed
}

const (
	DB_USER     = "swaveadmin"
	DB_PASSWORD = "swavepwd"
	DB_NAME     = "swave"
	DB_HOST     = "localhost"
	DB_PORT     = "5432"
)

var db *sql.DB

func main() {
	var err error
	db, err = createDBConnection()
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}
	defer db.Close()

	//Initialize Gin router
	r := gin.Default()

	// Define routes
	r.POST("/messages", createMessage)
	r.GET("/messages/:id", getMessage)
	r.GET("/messages", listMessages)

	//Start server
	log.Println("Starting server on :8080...")
	r.Run(":8080")
}

// Create a database connection
func createDBConnection() (*sql.DB, error) {
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME)
	return sql.Open("postgres", connStr)
}

func createMessage(c *gin.Context) {
	var msg Message
	if err := c.ShouldBindJSON(&msg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	id, err := insertMessage(msg.Message, msg.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create message"})
		return
	}

	msg.ID = id
	c.JSON(http.StatusCreated, msg)
}

// getMessage handles GET requests to "/api/hello"
func getMessage(c *gin.Context) {
	id := c.Param("id")
	msg, err := fetchMessageByID(id)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Message not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve message"})
		return
	}

	c.JSON(http.StatusOK, msg)
}

// listMessages handles GET requests to "/api/messages"
func listMessages(c *gin.Context) {
	messages, err := fetchAllMessages()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list messages"})
		return
	}

	c.JSON(http.StatusOK, messages)
}

//missing update and delete

// Database functions

// InsertMessage inserts a new message associated with a user ID
func insertMessage(message string, userID int) (int, error) {
	var id int
	err := db.QueryRow(
		"INSERT INTO messages (message, user_id) VALUES ($1, $2) RETURNING id",
		message, userID,
	).Scan(&id)
	return id, err
}

func fetchMessageByID(id string) (Message, error) {
	var msg Message
	err := db.QueryRow(
		"SELECT id, message, user_id FROM messages WHERE id = $1", id,
	).Scan(&msg.ID, &msg.Message, &msg.UserID)
	return msg, err
}

func fetchAllMessages() ([]Message, error) {
	rows, err := db.Query("SELECT id, message, user_id FROM messages")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.Message, &msg.UserID); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}
