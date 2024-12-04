package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/streadway/amqp"
)

// RabbitMQ configuration
var (
	RabbitMQURL      = "amqp://guest:guest@localhost:5672/"
	UserCreatedQueue = "user_created"
	UserUpdatedQueue = "user_updated"
	UserDeletedQueue = "user_deleted"
)

type Message struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
	UserID  int    `json:"user_id"`
}

type MessageWithUser struct {
	ID       int    `json:"id"`
	Message  string `json:"message"`
	Username string `json:"username"`
}

var (
	DbUser     = "swaveadmin"
	DbPassword = "swavepwd"
	DbName     = "swave"
	DbHost     = "localhost"
	DbPort     = "5432"
)

var (
	db       *sql.DB
	rabbitCh *amqp.Channel
)

func envRead() string {
	err := godotenv.Load(".env")

	if err != nil {
		log.Fatalf("Error loading .env file")
		return err.Error()
	}
	DbUser = os.Getenv("DB_USER")
	DbPassword = os.Getenv("DB_PASSWORD")
	DbName = os.Getenv("DB_NAME")
	DbHost = os.Getenv("DB_HOST")
	DbPort = os.Getenv("DB_PORT")

	RabbitMQURL = os.Getenv("RABBITMQ_URL")
	log.Printf("RabbitMQURL: %s", RabbitMQURL)
	return ""

}

func main() {
	envRead()
	var err error
	db, err = createDBConnection()
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}
	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			log.Printf("Error closing database connection: %v", err)
		}
	}(db)

	err = consumeRabbitMQ()
	if err != nil {
		log.Fatalf("Error consuming RabbitMQ messages: %v", err)
	}

	//Initialize Gin router
	r := gin.Default()

	// Add CORS middleware
	r.Use(CORSMiddleware())

	// Define routes
	r.POST("/messages", createMessage)
	r.GET("/messages/:id", getMessageWithUsername)
	r.GET("/messages", listMessagesWithUsers)
	r.GET("/messages/user/:userId", listUserMessages)
	r.PUT("/messages/:id", updateMessage)
	r.DELETE("/messages/:id", deleteMessage)

	//Start server
	log.Println("Starting server on :8080...")
	err = r.Run(":8080")
	if err != nil {
		return
	}
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// Create a database connection
func createDBConnection() (*sql.DB, error) {
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		DbHost, DbPort, DbUser, DbPassword, DbName)
	return sql.Open("postgres", connStr)
}

func consumeRabbitMQ() error {
	conn, err := amqp.Dial(RabbitMQURL)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}
	rabbitCh, err = conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open RabbitMQ channel: %w", err)
	}

	// Declare the exchange
	err = rabbitCh.ExchangeDeclare(
		"user_events", // Exchange name
		"fanout",      // Type
		true,          // Durable
		false,         // Auto-delete
		false,         // Internal
		false,         // No-wait
		nil,           // Arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	// Declare the queue
	q, err := rabbitCh.QueueDeclare(
		"",    // Generate a random queue name
		true,  // Durable
		false, // Auto-delete
		true,  // Exclusive
		false, // No-wait
		nil,   // Arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// Bind the queue to the exchange
	err = rabbitCh.QueueBind(
		q.Name,        // Queue name
		"",            // Routing key
		"user_events", // Exchange name
		false,         // No-wait
		nil,           // Arguments
	)
	if err != nil {
		return fmt.Errorf("failed to bind queue: %w", err)
	}

	// Start consuming messages
	msgs, err := rabbitCh.Consume(
		q.Name, // Queue name
		"",     // Consumer tag
		true,   // Auto-ack
		false,  // Exclusive
		false,  // No-local
		false,  // No-wait
		nil,    // Arguments
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	go func() {
		for msg := range msgs {
			handleMessage(msg)
		}
	}()

	return nil
}

func handleMessage(msg amqp.Delivery) {
	var event map[string]string
	err := json.Unmarshal(msg.Body, &event)
	if err != nil {
		log.Printf("Error parsing message: %v", err)
		return
	}

	userID, ok := event["user_id"]
	if !ok || userID == "" {
		log.Printf("Error: missing 'user_id' in event payload")
		return
	}

	username, _ := event["username"]
	//if !ok || username == "" {
	//	log.Printf("Error: missing 'username' in event payload")
	//	return
	//}

	switch event["event_type"] { // use `event["event_type"]` or msg.RoutingKey
	case UserCreatedQueue:
		err = insertUserReplica(userID, username)
		if err != nil {
			log.Printf("Error inserting user replica: %v", err)
		}

	case UserUpdatedQueue:
		err = updateUserReplica(userID, username)
		if err != nil {
			log.Printf("Error updating user replica: %v", err)
		}

	case UserDeletedQueue:
		err = deleteUserReplica(userID)
		if err != nil {
			log.Printf("Error deleting user replica: %v", err)
		}

	default:
		log.Printf("Unhandled message type: %s", event["event_type"])
	}
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

// getMessageWithUsername handles GET requests to "/messages/:id"
func getMessageWithUsername(c *gin.Context) {
	id := c.Param("id")
	msg, err := fetchMessageWithUsername(id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Message not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve message"})
		return
	}

	c.JSON(http.StatusOK, msg)
}

func listUserMessages(c *gin.Context) {
	userId := c.Param("userId")
	messages, err := fetchAllMessagesByUserId(userId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user messages"})
		return
	}

	c.JSON(http.StatusOK, messages)
}

func listMessages(c *gin.Context) {
	messages, err := fetchAllMessages()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list messages"})
		return
	}

	c.JSON(http.StatusOK, messages)
}

// listMessagesWithUsers handles GET requests to "/messages"
func listMessagesWithUsers(c *gin.Context) {
	messages, err := fetchAllMessagesWithUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list messages"})
		return
	}

	c.JSON(http.StatusOK, messages)
}

func updateMessage(c *gin.Context) {
	id := c.Param("id")
	var message Message
	if err := c.ShouldBindJSON(&message); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updatedMessage, err := updateMessageDetails(id, message.Message)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Message not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update message"})
		return
	}

	c.JSON(http.StatusOK, updatedMessage)
}

func deleteMessage(c *gin.Context) {
	id := c.Param("id")

	rowsAffected, err := deleteMessageByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete message"})
		return
	}
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Message not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Message deleted"})
}

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

func fetchMessageWithUsername(id string) (MessageWithUser, error) {
	var msg MessageWithUser
	err := db.QueryRow(
		`SELECT m.id, m.message, u.username 
         FROM messages m 
         JOIN user_replica u ON m.user_id = u.id 
         WHERE m.id = $1`, id,
	).Scan(&msg.ID, &msg.Message, &msg.Username)

	if err != nil {
		log.Printf("Error fetching message with username: %v", err)
		return msg, err
	}

	return msg, nil
}

// currently no endpoint
func fetchAllMessages() ([]Message, error) {
	rows, err := db.Query("SELECT id, message, user_id FROM messages")
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}(rows)

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

func fetchAllMessagesWithUsers() ([]MessageWithUser, error) {
	rows, err := db.Query(`
        SELECT m.id, m.message, u.username 
        FROM messages m 
        JOIN user_replica u ON m.user_id = u.id
    `)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}(rows)

	var messages []MessageWithUser
	for rows.Next() {
		var msg MessageWithUser
		if err := rows.Scan(&msg.ID, &msg.Message, &msg.Username); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func fetchAllMessagesByUserId(userId string) ([]Message, error) {
	rows, err := db.Query(
		`SELECT id, message, user_id
			FROM messages
			WHERE user_id = $1`, userId,
	)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}(rows)

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

func updateMessageDetails(id string, messageBody string) (Message, error) {
	var message Message
	err := db.QueryRow(
		"UPDATE messages SET message = $2 WHERE id = $1 RETURNING id, message, user_id",
		id, messageBody,
	).Scan(&message.ID, &message.Message, &message.UserID)
	return message, err
}

func deleteMessageByID(id string) (int64, error) {
	result, err := db.Exec("DELETE FROM messages WHERE id = $1", id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func insertUserReplica(id string, userName string) error {
	_, err := db.Exec(
		"INSERT INTO user_replica (id, username) VALUES ($1, $2)",
		id, userName,
	)
	if err != nil {
		log.Printf("Error inserting user replica: %v", err)
	}
	return err
}

func updateUserReplica(id string, userName string) error {
	_, err := db.Exec(
		"UPDATE user_replica SET username = $2 WHERE id = $1",
		id, userName,
	)
	if err != nil {
		log.Printf("Error updating user replica: %v", err)
	}
	return err
}

func deleteUserReplica(id string) error {
	userID, err := strconv.Atoi(id)
	if err != nil {
		log.Printf("Error converting id to integer: %v", err)
		return err
	}

	_, err = db.Exec("DELETE FROM user_replica WHERE id = $1",
		userID,
	)
	if err != nil {
		log.Printf("Error deleting user replica: %v", err)
	}
	return err
}

// getUserReplica retrieves a user replica from the database
// given a user ID and returns any error encountered.
func getUserReplica(id string) error {
	_, err := db.Exec(
		"SELECT id, username FROM user_replica WHERE id = $1",
		id,
	)

	if err != nil {
		log.Printf("Error fetching user replica: %v", err)
	}

	return err
}
