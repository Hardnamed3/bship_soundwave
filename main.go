package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"os"
	"strconv"
	"time"

	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/streadway/amqp"

	"github.com/prometheus/client_golang/prometheus"
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

// Database configuration
var (
	DbUser     = "swaveadmin"
	DbPassword = "swavepwd"
	DbName     = "swave"
	DbHost     = "localhost"
	DbPort     = "5432"
)

// Prometheus metrics
var (
	messageCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "messages_total",
			Help: "Total number of messages processed",
		},
		[]string{"operation"},
	)

	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "Duration of HTTP requests",
		},
		[]string{"handler", "method"},
	)
)

var (
	db       *sql.DB
	rabbitCh *amqp.Channel
)

func envRead() string {
	err := godotenv.Load(".env")
	if err != nil {
		fmt.Printf("Warning: Error loading .env file. Falling back to os.Getenv: %v", err)
	}

	DbUser = os.Getenv("DB_USER")
	fmt.Printf("DbUser: %s\n", DbUser)
	DbPassword = os.Getenv("DB_PASSWORD")
	fmt.Printf("dbPassword: %s\n", DbPassword)
	DbName = os.Getenv("DB_NAME")
	fmt.Printf("DbName: %s\n", DbName)
	DbHost = os.Getenv("DB_HOST")
	fmt.Printf("DbHost: %s\n", DbHost)
	DbPort = os.Getenv("DB_PORT")
	fmt.Printf("DbPort: %s\n", DbPort)

	RabbitMQURL = os.Getenv("RABBITMQ_URL")
	fmt.Printf("RabbitMQURL: %s\n", RabbitMQURL)
	return ""

}

func initMetrics() {
	prometheus.MustRegister(messageCounter)
	prometheus.MustRegister(requestDuration)
}

func main() {
	//environment and metrics
	envRead()
	initMetrics()

	//Connect to database
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

	//Connect to RabbitMQ
	err = consumeRabbitMQ()
	if err != nil {
		log.Fatalf("Error consuming RabbitMQ messages: %v", err)
	}

	//Initialize Gin router
	r := gin.Default()

	// Add CORS middleware
	r.Use(CORSMiddleware())

	// Initialize Prometheus middleware
	r.Use(metricsMiddleware())
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

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
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
		}
		c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
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

func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = "undefined" // Handle cases where FullPath() is nil
		}

		c.Next()

		duration := time.Since(start).Seconds()
		requestDuration.WithLabelValues(path, c.Request.Method).Observe(duration)
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
	start := time.Now()

	var msg Message
	if err := c.ShouldBindJSON(&msg); err != nil {
		messageCounter.WithLabelValues("bad_request").Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	id, err := insertMessage(msg.Message, msg.UserID)
	if err != nil {
		messageCounter.WithLabelValues("internal_error").Inc()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create message"})
		return
	}

	msg.ID = id

	messageCounter.WithLabelValues("success").Inc()
	c.JSON(http.StatusCreated, msg)

	//record the request duration
	duration := time.Since(start).Seconds()
	requestDuration.WithLabelValues("/messages", c.Request.Method).Observe(duration)
}

// getMessageWithUsername handles GET requests to "/messages/:id"
func getMessageWithUsername(c *gin.Context) {
	start := time.Now()

	id := c.Param("id")
	msg, err := fetchMessageWithUsername(id)
	if errors.Is(err, sql.ErrNoRows) {
		messageCounter.WithLabelValues("not_found").Inc()
		c.JSON(http.StatusNotFound, gin.H{"error": "Message not found"})
		return
	} else if err != nil {
		messageCounter.WithLabelValues("internal_error").Inc()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve message"})
		return
	}

	messageCounter.WithLabelValues("success").Inc()
	c.JSON(http.StatusOK, msg)

	//record the request duration
	duration := time.Since(start).Seconds()
	requestDuration.WithLabelValues("/messages/:id", c.Request.Method).Observe(duration)
}

func listUserMessages(c *gin.Context) {
	start := time.Now()

	userId := c.Param("userId")
	messages, err := fetchAllMessagesByUserId(userId)
	if err != nil {
		messageCounter.WithLabelValues("internal_error").Inc()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user messages"})
		return
	}

	messageCounter.WithLabelValues("success").Inc()
	c.JSON(http.StatusOK, messages)

	//record the request duration
	duration := time.Since(start).Seconds()
	requestDuration.WithLabelValues("/messages/:userId", c.Request.Method).Observe(duration)
}

func listMessages(c *gin.Context) {
	start := time.Now()

	messages, err := fetchAllMessages()
	if err != nil {
		messageCounter.WithLabelValues("internal_error").Inc()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list messages"})
		return
	}

	messageCounter.WithLabelValues("success").Inc()
	c.JSON(http.StatusOK, messages)

	//record the request duration
	duration := time.Since(start).Seconds()
	requestDuration.WithLabelValues("/messages", c.Request.Method).Observe(duration)
}

// listMessagesWithUsers handles GET requests to "/messages"
func listMessagesWithUsers(c *gin.Context) {
	start := time.Now()

	messages, err := fetchAllMessagesWithUsers()
	if err != nil {
		messageCounter.WithLabelValues("internal_error").Inc()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list messages"})
		return
	}

	messageCounter.WithLabelValues("success").Inc()
	c.JSON(http.StatusOK, messages)

	//record the request duration
	duration := time.Since(start).Seconds()
	requestDuration.WithLabelValues("/messages", c.Request.Method).Observe(duration)
}

func updateMessage(c *gin.Context) {
	start := time.Now()

	id := c.Param("id")
	var message Message
	if err := c.ShouldBindJSON(&message); err != nil {
		messageCounter.WithLabelValues("bad_request").Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updatedMessage, err := updateMessageDetails(id, message.Message)
	if errors.Is(err, sql.ErrNoRows) {
		messageCounter.WithLabelValues("not_found").Inc()
		c.JSON(http.StatusNotFound, gin.H{"error": "Message not found"})
		return
	} else if err != nil {
		messageCounter.WithLabelValues("internal_error").Inc()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update message"})
		return
	}

	messageCounter.WithLabelValues("success").Inc()
	c.JSON(http.StatusOK, updatedMessage)

	//record the request duration
	duration := time.Since(start).Seconds()
	requestDuration.WithLabelValues("/messages/:id", c.Request.Method).Observe(duration)
}

func deleteMessage(c *gin.Context) {
	start := time.Now()
	id := c.Param("id")

	rowsAffected, err := deleteMessageByID(id)
	if err != nil {
		messageCounter.WithLabelValues("internal_error").Inc()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete message"})
		return
	}
	if rowsAffected == 0 {
		messageCounter.WithLabelValues("not_found").Inc()
		c.JSON(http.StatusNotFound, gin.H{"error": "Message not found"})
		return
	}

	messageCounter.WithLabelValues("success").Inc()
	c.JSON(http.StatusOK, gin.H{"message": "Message deleted"})

	//record the request duration
	duration := time.Since(start).Seconds()
	requestDuration.WithLabelValues("/messages/:id", c.Request.Method).Observe(duration)
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
