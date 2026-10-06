package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	SMTP     SMTPConfig     `mapstructure:"smtp"`
	JWT      JWTConfig      `mapstructure:"jwt"`
}

type ServerConfig struct {
	Port         int      `mapstructure:"port"`
	CORSOrigins  []string `mapstructure:"cors_origins"`
	CookieSecure bool     `mapstructure:"cookie_secure"` // 生产 HTTPS 置 true
}

type DatabaseConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Username        string        `mapstructure:"username"`
	Password        string        `mapstructure:"password"`
	DBName          string        `mapstructure:"dbname"`
	SSLMode         string        `mapstructure:"sslmode"`
	Timezone        string        `mapstructure:"timezone"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

type JWTConfig struct {
	Secret string `mapstructure:"secret"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type SMTPConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
}

func loadDotEnv() {
	seen := map[string]struct{}{}
	var paths []string
	if wd, err := os.Getwd(); err == nil {
		dir := wd
		for i := 0; i < 5; i++ {
			paths = append(paths, filepath.Join(dir, ".env"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	paths = append(paths, ".env")
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		_ = godotenv.Load(abs)
		return
	}
}

func Load() (*Config, error) {
	loadDotEnv()

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("configs")
	v.AddConfigPath(".")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetDefault("server.port", 8199)
	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("smtp.host", "smtp.163.com")
	v.SetDefault("smtp.port", 465)
	_ = v.BindEnv("smtp.username", "SMTP_USERNAME")
	_ = v.BindEnv("smtp.password", "SMTP_PASSWORD")
	_ = v.BindEnv("smtp.from", "SMTP_FROM")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件 configs/config.yaml 失败: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8199
	}
	if cfg.JWT.Secret == "" {
		return nil, fmt.Errorf("jwt.secret 为空")
	}
	if len(cfg.Server.CORSOrigins) == 0 {
		cfg.Server.CORSOrigins = []string{"http://localhost:8080", "http://127.0.0.1:8080"}
	}
	if cfg.Redis.Addr == "" {
		cfg.Redis.Addr = "localhost:6379"
	}
	if cfg.SMTP.Host == "" {
		cfg.SMTP.Host = "smtp.163.com"
	}
	if cfg.SMTP.Port == 0 {
		cfg.SMTP.Port = 465
	}
	if u := os.Getenv("SMTP_USERNAME"); u != "" {
		cfg.SMTP.Username = u
	}
	if p := os.Getenv("SMTP_PASSWORD"); p != "" {
		cfg.SMTP.Password = p
	}
	if f := os.Getenv("SMTP_FROM"); f != "" {
		cfg.SMTP.From = f
	} else if cfg.SMTP.From == "" || cfg.SMTP.From == "your_account@163.com" {
		cfg.SMTP.From = cfg.SMTP.Username
	}
	return &cfg, nil
}
