package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"full-stack-learning/apps/api/internal/config"
	"full-stack-learning/apps/api/internal/database"
)

func main() {
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

	fmt.Fprintf(os.Stdout, "数据库连接成功：%s:%s/%s\n", cfg.Database.Host, cfg.Database.Port, cfg.Database.Name)
}
