package config

import (
	"log"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

type Config struct {
	Port string
}

func Load() *Config {
	workspaceRoot := filepath.Join("..", "..", ".env")
	if err := godotenv.Load(workspaceRoot); err != nil {
		log.Printf("Warning: .env file not found at %s, using defaults", workspaceRoot)
	}

	port := os.Getenv("WEBSITE_PORT")
	if port == "" {
		port = "4002"
	}

	if port[0] != ':' {
		port = ":" + port
	}

	return &Config{
		Port: port,
	}
}
