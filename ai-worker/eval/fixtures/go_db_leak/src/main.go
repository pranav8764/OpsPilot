package main

import (
	"database/sql"
	"fmt"
	"log"
	_ "github.com/lib/pq"
)

func getUserEmail(db *sql.DB, userID int) (string, error) {
	rows, err := db.Query("SELECT email FROM users WHERE id = $1", userID)
	if err != nil {
		return "", err
	}
	// Missing: defer rows.Close()

	var email string
	if rows.Next() {
		err = rows.Scan(&email)
		if err != nil {
			return "", err
		}
	}
	return email, nil
}

func main() {
	db, err := sql.Open("postgres", "postgres://opspilot:secret@localhost/db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	for i := 0; i < 1000; i++ {
		email, err := getUserEmail(db, 1)
		if err != nil {
			log.Printf("Error: %v", err)
		} else {
			fmt.Println("Email:", email)
		}
	}
}
