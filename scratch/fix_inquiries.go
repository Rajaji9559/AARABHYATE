package main

import (
	"fmt"
	"log"
	"os"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	_ = godotenv.Load("c:/Users/Rajaram mishra/Desktop/aarabhyate/.env")

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		log.Fatalf("Connect error: %v", err)
	}
	defer db.Close()

	fmt.Println("--- Fixing project_inquiries table schema and data ---")
	
	// Start a transaction
	tx, err := db.Beginx()
	if err != nil {
		log.Fatalf("Beginx error: %v", err)
	}
	defer tx.Rollback()

	// Update existing NULL budget_estimate to ''
	_, err = tx.Exec("UPDATE project_inquiries SET budget_estimate = '' WHERE budget_estimate IS NULL")
	if err != nil {
		log.Fatalf("Update budget_estimate error: %v", err)
	}
	_, err = tx.Exec("ALTER TABLE project_inquiries ALTER COLUMN budget_estimate SET DEFAULT ''")
	if err != nil {
		log.Fatalf("Alter budget_estimate default error: %v", err)
	}
	_, err = tx.Exec("ALTER TABLE project_inquiries ALTER COLUMN budget_estimate SET NOT NULL")
	if err != nil {
		log.Fatalf("Alter budget_estimate not null error: %v", err)
	}

	// Update existing NULL timeline to ''
	_, err = tx.Exec("UPDATE project_inquiries SET timeline = '' WHERE timeline IS NULL")
	if err != nil {
		log.Fatalf("Update timeline error: %v", err)
	}
	_, err = tx.Exec("ALTER TABLE project_inquiries ALTER COLUMN timeline SET DEFAULT ''")
	if err != nil {
		log.Fatalf("Alter timeline default error: %v", err)
	}
	_, err = tx.Exec("ALTER TABLE project_inquiries ALTER COLUMN timeline SET NOT NULL")
	if err != nil {
		log.Fatalf("Alter timeline not null error: %v", err)
	}

	// Update existing NULL admin_notes to ''
	_, err = tx.Exec("UPDATE project_inquiries SET admin_notes = '' WHERE admin_notes IS NULL")
	if err != nil {
		log.Fatalf("Update admin_notes error: %v", err)
	}
	_, err = tx.Exec("ALTER TABLE project_inquiries ALTER COLUMN admin_notes SET DEFAULT ''")
	if err != nil {
		log.Fatalf("Alter admin_notes default error: %v", err)
	}
	_, err = tx.Exec("ALTER TABLE project_inquiries ALTER COLUMN admin_notes SET NOT NULL")
	if err != nil {
		log.Fatalf("Alter admin_notes not null error: %v", err)
	}

	// Update existing NULL ip_address to '127.0.0.1'
	_, err = tx.Exec("UPDATE project_inquiries SET ip_address = '127.0.0.1' WHERE ip_address IS NULL")
	if err != nil {
		log.Fatalf("Update ip_address error: %v", err)
	}
	_, err = tx.Exec("ALTER TABLE project_inquiries ALTER COLUMN ip_address SET DEFAULT '127.0.0.1'")
	if err != nil {
		log.Fatalf("Alter ip_address default error: %v", err)
	}
	_, err = tx.Exec("ALTER TABLE project_inquiries ALTER COLUMN ip_address SET NOT NULL")
	if err != nil {
		log.Fatalf("Alter ip_address not null error: %v", err)
	}

	err = tx.Commit()
	if err != nil {
		log.Fatalf("Commit error: %v", err)
	}

	fmt.Println("Project inquiries database schema fixed successfully!")
}
