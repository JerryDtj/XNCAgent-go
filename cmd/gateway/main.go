package main

import (
	"fmt"
	"log"

	"github.com/JerryDtj/XNCAgent-go/internal/config"
	"github.com/JerryDtj/XNCAgent-go/internal/database"
	"github.com/JerryDtj/XNCAgent-go/internal/middleware"
	"github.com/JerryDtj/XNCAgent-go/internal/proxy"
	"github.com/JerryDtj/XNCAgent-go/internal/user"
	"github.com/JerryDtj/XNCAgent-go/pkg/response"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := database.Open(cfg.Database)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close(db)

	rdb, err := database.OpenRedis(cfg.Redis)
	if err != nil {
		log.Fatalf("open redis: %v", err)
	}
	defer rdb.Close()

	router := gin.Default()
	router.Use(middleware.CORS(cfg.Server.CORSOrigins))
	router.Use(middleware.JWT(cfg.JWT.Secret))
	router.GET("/health", func(c *gin.Context) {
		response.OK(c, gin.H{"status": "up"})
	})
	user.RegisterRoutes(router, db, rdb, cfg.JWT.Secret, cfg.SMTP)
	agentProxy, err := proxy.Agent("http://127.0.0.1:18000")
	if err != nil {
		log.Fatalf("agent proxy: %v", err)
	}
	router.Any("/agent/*path", agentProxy)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	if err := router.Run(addr); err != nil {
		log.Fatal(err)
	}
}
