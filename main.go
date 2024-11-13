package main

import (
	"database/sql"
	"encoding/json"
	_ "github.com/lib/pq"
	"log"
	"net/http"
)

type Message struct {
	Message string `json:"message"`
}

func main() {
	http.HandleFunc("/api/hello", HelloHandler)
	http.HandleFunc("/api/messages", MessagesHandler)

	log.Println("Starting server on :8080...")
	http.ListenAndServe(":8080", nil)
}

func HelloHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == http.MethodPost {
		var msg Message
		json.NewDecoder(r.Body).Decode(&msg)
		// Insert into PostgreSQL (implement below)
		InsertMessage(msg.Message)
		json.NewEncoder(w).Encode(msg)
	} else if r.Method == http.MethodGet {
		// Retrieve from PostgreSQL (implement below)
		message := GetMessage()
		json.NewEncoder(w).Encode(Message{Message: message})
	}
}

// MessagesHandler handles the GET request for "/api/messages"
func MessagesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		// Pre-flight request for CORS
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == http.MethodGet {
		// Retrieve all messages from PostgreSQL
		messages := GetAllMessages()
		// Respond with all messages
		json.NewEncoder(w).Encode(messages)
	}
}

// Database functions
func InsertMessage(message string) {
	db, err := sql.Open("postgres", "postgres://swaveadmin:swavepwd@postgres-db:5432/swave?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec("INSERT INTO messages (message) VALUES ($1)", message)
	if err != nil {
		log.Fatal(err)
	}
}

func GetMessage() string {
	db, err := sql.Open("postgres", "postgres://swaveadmin:swavepwd@postgres-db:5432/swave?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	var message string
	db.QueryRow("SELECT message FROM messages ORDER BY id DESC LIMIT 1").Scan(&message)
	return message
}

// GetAllMessages retrieves all messages from the database
func GetAllMessages() []Message {
	db, err := sql.Open("postgres", "postgres://swaveadmin:swavepwd@postgres-db:5432/swave?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT message FROM messages")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.Message); err != nil {
			log.Fatal(err)
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	return messages
}
