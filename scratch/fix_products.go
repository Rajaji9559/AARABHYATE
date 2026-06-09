package main

import (
	"fmt"
	"log"
	"os"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type TempProduct struct {
	ID          string  `db:"id"`
	Name        string  `db:"name"`
	Category    *string `db:"category"`
	SKU         *string `db:"sku"`
	IsActive    *bool   `db:"is_active"`
}

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

	fmt.Println("--- Current products check ---")
	var products []TempProduct
	err = db.Select(&products, "SELECT id, name, category, sku, is_active FROM products")
	if err != nil {
		log.Fatalf("Select products error: %v", err)
	}

	for _, p := range products {
		catStr := "NULL"
		if p.Category != nil {
			catStr = *p.Category
		}
		skuStr := "NULL"
		if p.SKU != nil {
			skuStr = *p.SKU
		}
		activeStr := "NULL"
		if p.IsActive != nil {
			activeStr = fmt.Sprintf("%t", *p.IsActive)
		}
		fmt.Printf("ID: %s | Name: %s | Category: %s | SKU: %s | IsActive: %s\n", p.ID, p.Name, catStr, skuStr, activeStr)
	}

	fmt.Println("\n--- Fixing products table schema and data ---")
	
	// Start a transaction
	tx, err := db.Beginx()
	if err != nil {
		log.Fatalf("Beginx error: %v", err)
	}
	defer tx.Rollback()

	// Update existing NULL category and sku to ''
	_, err = tx.Exec("UPDATE products SET category = '' WHERE category IS NULL")
	if err != nil {
		log.Fatalf("Update category error: %v", err)
	}

	_, err = tx.Exec("UPDATE products SET sku = '' WHERE sku IS NULL")
	if err != nil {
		log.Fatalf("Update sku error: %v", err)
	}

	_, err = tx.Exec("UPDATE products SET is_active = TRUE WHERE is_active IS NULL")
	if err != nil {
		log.Fatalf("Update is_active error: %v", err)
	}

	// Set category to NOT NULL DEFAULT ''
	_, err = tx.Exec("ALTER TABLE products ALTER COLUMN category SET DEFAULT ''")
	if err != nil {
		log.Fatalf("Alter category default error: %v", err)
	}
	_, err = tx.Exec("ALTER TABLE products ALTER COLUMN category SET NOT NULL")
	if err != nil {
		log.Fatalf("Alter category not null error: %v", err)
	}

	// Set sku to NOT NULL DEFAULT ''
	_, err = tx.Exec("ALTER TABLE products ALTER COLUMN sku SET DEFAULT ''")
	if err != nil {
		log.Fatalf("Alter sku default error: %v", err)
	}
	_, err = tx.Exec("ALTER TABLE products ALTER COLUMN sku SET NOT NULL")
	if err != nil {
		log.Fatalf("Alter sku not null error: %v", err)
	}

	// Set is_active to NOT NULL DEFAULT TRUE
	_, err = tx.Exec("ALTER TABLE products ALTER COLUMN is_active SET DEFAULT TRUE")
	if err != nil {
		log.Fatalf("Alter is_active default error: %v", err)
	}
	_, err = tx.Exec("ALTER TABLE products ALTER COLUMN is_active SET NOT NULL")
	if err != nil {
		log.Fatalf("Alter is_active not null error: %v", err)
	}

	// Update the Festive Chariot Model category specifically to 'controllers' or 'actuators' so it is visible in one of the filters
	_, err = tx.Exec("UPDATE products SET category = 'controllers', sku = 'CHARIOT-MODEL-001' WHERE name = 'Festive Chariot Model (Precision Scale)'")
	if err != nil {
		log.Fatalf("Update Festive Chariot category error: %v", err)
	}

	err = tx.Commit()
	if err != nil {
		log.Fatalf("Commit error: %v", err)
	}

	fmt.Println("Database schema fixed successfully!")
}
