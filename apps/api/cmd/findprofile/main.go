package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5"

	"full-stack-learning/apps/api/internal/config"
	"full-stack-learning/apps/api/internal/database"
	"full-stack-learning/apps/api/internal/profile"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("用法: go run ./cmd/findprofile <username>")
	}

	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load configuration: %v", err)
	}

	pool, err := database.NewPostgresPool(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("connect to PostgreSQL: %v", err)
	}
	defer pool.Close()

	repository := profile.NewRepository(pool)
	result, err := repository.FindByUsername(ctx, os.Args[1])
	if errors.Is(err, pgx.ErrNoRows) {
		log.Fatalf("没有找到用户名 %q", os.Args[1])
	}
	if err != nil {
		log.Fatalf("query profile: %v", err)
	}

	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatalf("encode profile: %v", err)
	}

	fmt.Fprintln(os.Stdout, string(output))
}
