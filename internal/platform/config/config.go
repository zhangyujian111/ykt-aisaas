// Package config 加载平台配置（yaml + 环境变量覆盖）。
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config 平台根配置。
type Config struct {
	Server   Server   `mapstructure:"server"`
	MySQL    MySQL    `mapstructure:"mysql"`
	Postgres Postgres `mapstructure:"postgres"`
	Redis    Redis    `mapstructure:"redis"`
	Crypto   Crypto   `mapstructure:"crypto"`
	Tenant   Tenant   `mapstructure:"tenant"`
	Quota    Quota    `mapstructure:"quota"`
	Metering Metering `mapstructure:"metering"`
	Log      Log      `mapstructure:"log"`
}

type Server struct {
	Port          int    `mapstructure:"port"`
	InternalToken string `mapstructure:"internalToken"`
}

type MySQL struct {
	DSN            string `mapstructure:"dsn"`
	MaxOpenConns   int    `mapstructure:"maxOpenConns"`
	MaxIdleConns   int    `mapstructure:"maxIdleConns"`
	ConnMaxLifeSec int    `mapstructure:"connMaxLifeSec"`
}

type Postgres struct {
	DSN      string `mapstructure:"dsn"`
	MaxConns int    `mapstructure:"maxConns"`
}

type Redis struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type Crypto struct {
	AESKey string `mapstructure:"aesKey"` // 32 字节，model_registry.apiKeyEnc 解密
}

type Tenant struct {
	SkipTables []string `mapstructure:"skipTables"`
}

type Quota struct {
	Precheck bool `mapstructure:"precheck"`
}

type Metering struct {
	BatchSize int `mapstructure:"batchSize"`
	FlushSec  int `mapstructure:"flushSec"`
}

type Log struct {
	Level string `mapstructure:"level"` // debug/info/warn/error
}

// Load 按路径加载配置。env 覆盖规则：AISAA_{路径下划线连接}，
// 如 AISAA_MYSQL_DSN、AISAA_SERVER_INTERNALTOKEN。
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.SetEnvPrefix("AISAA")
	v.AutomaticEnv()

	setDefaults(v)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.port", 8190)
	v.SetDefault("server.internalToken", "dev-internal-token")
	v.SetDefault("mysql.maxOpenConns", 40)
	v.SetDefault("postgres.dsn", "postgres://aisaas:aisaas@127.0.0.1:15432/ykt_aisaas_rag")
	v.SetDefault("postgres.maxConns", 8)
	v.SetDefault("mysql.maxIdleConns", 10)
	v.SetDefault("mysql.connMaxLifeSec", 3600)
	v.SetDefault("redis.addr", "127.0.0.1:6379")
	v.SetDefault("redis.db", 0)
	v.SetDefault("quota.precheck", true)
	v.SetDefault("metering.batchSize", 200)
	v.SetDefault("metering.flushSec", 3)
	v.SetDefault("log.level", "info")
	v.SetDefault("crypto.aesKey", "dev-aes-key-32-bytes-1234567890a") // 32 bytes
	v.SetDefault("tenant.skipTables", []string{
		"ykt_aisaas_tenant",
		"ykt_aisaas_plan",
		"ykt_aisaas_dict_type",
		"ykt_aisaas_dict_item",
		"ykt_aisaas_admin_user",
		"ykt_aisaas_admin_role",
		"ykt_aisaas_balance",
		"ykt_aisaas_balance_transaction",
		"ykt_aisaas_internal_tenant_config",
		"ykt_aisaas_model_registry",
	})
}
