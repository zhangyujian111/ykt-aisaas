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
	Audit    Audit    `mapstructure:"audit"`
	Log      Log           `mapstructure:"log"`
	OTel     OTelConfig     `mapstructure:"otel"`
	Anomaly  AnomalyConfig  `mapstructure:"anomaly"`
	// Vision 多模态视觉理解配置
	Vision    Vision    `mapstructure:"vision"`
	DashScope DashScope `mapstructure:"dashscope"`
	Zhipu     Zhipu     `mapstructure:"zhipu"`
	OpenAI    OpenAI    `mapstructure:"openai"`
	// === V5-S: OAuth 2.1 + SCIM 配置 ===
	OAuth2 OAuth2Config `mapstructure:"oauth2"`
	Scim   ScimConfig   `mapstructure:"scim"`
}

// OAuth2Config OAuth 2.1 / SCIM 配置。
type OAuth2Config struct {
	ClientID     string `mapstructure:"clientId"`
	ClientSecret string `mapstructure:"clientSecret"`
	DPoPEnabled  bool   `mapstructure:"dpopEnabled"`
}

// ScimConfig SCIM 服务配置。
type ScimConfig struct {
	AuthToken string `mapstructure:"authToken"`
}

// AnomalyConfig 异常检测配置。
type AnomalyConfig struct {
	Enabled         bool    `mapstructure:"enabled"`
	SlackWebhookURL string  `mapstructure:"slackWebhookUrl"`
	WebhookURLs     []string `mapstructure:"webhookUrls"`
	EmailSMTPHost   string  `mapstructure:"emailSmtpHost"`
	EmailSMTPPort   int     `mapstructure:"emailSmtpPort"`
	EmailFrom       string  `mapstructure:"emailFrom"`
	EmailTo         []string `mapstructure:"emailTo"`
	// === V5-M ML 异常检测（Isolation Forest）===
	MLEnabled        bool    `mapstructure:"mlEnabled"`
	MLThreshold      float64 `mapstructure:"mlThreshold"`      // 默认 0.6
	MLModelDir      string  `mapstructure:"mlModelDir"`      // 默认 "models/anomaly/"
	MLTrainInterval  int     `mapstructure:"mlTrainInterval"` // 默认 24（小时）
}

// Vision 视觉理解配置
type Vision struct {
	DefaultModel string `mapstructure:"defaultModel"` // 默认模型
}

// DashScope 阿里云 DashScope 配置（通义千问 VL）
type DashScope struct {
	APIKey string `mapstructure:"apiKey"`
}

// Zhipu 智谱 AI 配置（GLM-4V）
type Zhipu struct {
	APIKey string `mapstructure:"apiKey"`
}

// OpenAI OpenAI 配置（GPT-4V）
type OpenAI struct {
	APIKey string `mapstructure:"apiKey"`
}

// Audit 审计日志配置。
type Audit struct {
	StreamName    string `mapstructure:"streamName"`
	BatchSize     int    `mapstructure:"batchSize"`
	FlushSec      int    `mapstructure:"flushSec"`
	StreamMaxLen  int64  `mapstructure:"streamMaxLen"`
	DLQMaxRetries int    `mapstructure:"dlqMaxRetries"`
	Region        string `mapstructure:"region"`
}

type Server struct {
	Port          int    `mapstructure:"port"`
	BaseURL       string `mapstructure:"baseURL"`        // 基础 URL（OAuth2 issuer 使用）
	InternalToken string `mapstructure:"internalToken"`
	// TLS 配置（NICE v2 mTLS，Phase 3 公网部署）
	// 推荐通过 --tls-cert / --tls-key / --tls-ca 启动参数注入（避免落入 config.yaml）
	// 也可写 yaml（mapstructure 不忽略），生产建议用 flag + Secret 注入
	TLSCertFile   string `mapstructure:"tlsCertFile"`
	TLSKeyFile    string `mapstructure:"tlsKeyFile"`
	TLSCACertFile string `mapstructure:"tlsCaCertFile"`
	TLSPort       int    `mapstructure:"tlsPort"`
}

// IsTLSEnabled 判断是否启用 mTLS HTTPS 监听器。
// 触发条件：TLSCertFile + TLSKeyFile + TLSCACertFile 三项均非空。
func (s *Server) IsTLSEnabled() bool {
	return s.TLSCertFile != "" && s.TLSKeyFile != "" && s.TLSCACertFile != ""
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
	BatchSize     int    `mapstructure:"batchSize"`
	FlushSec      int    `mapstructure:"flushSec"`
	UseStream     bool   `mapstructure:"useStream"`
	StreamMaxLen  int64  `mapstructure:"streamMaxLen"`
	ConsumerGroup string `mapstructure:"consumerGroup"`
	DLQMaxRetries int    `mapstructure:"dlqMaxRetries"`
}

type Log struct {
	Level string `mapstructure:"level"` // debug/info/warn/error
}

// OTelConfig OpenTelemetry 配置（V4-O 阶段）。
type OTelConfig struct {
	ExporterURL    string  `mapstructure:"exporterURL"`    // "otel-collector:4317"
	SampleRatio    float64 `mapstructure:"sampleRatio"`    // trace 采样率
	MetricsEnabled bool    `mapstructure:"metricsEnabled"` // 默认 true
	LogsEnabled    bool    `mapstructure:"logsEnabled"`    // 默认 true
	ExportInterval int     `mapstructure:"exportInterval"` // 默认 15s
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
	v.SetDefault("server.tlsPort", 8443)
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
	v.SetDefault("metering.useStream", true)
	v.SetDefault("metering.streamMaxLen", 100000)
	v.SetDefault("metering.consumerGroup", "aisaas-metering-consumers")
	v.SetDefault("metering.dlqMaxRetries", 3)
	v.SetDefault("audit.streamName", "aisaas:audit:stream")
	v.SetDefault("audit.batchSize", 100)
	v.SetDefault("audit.flushSec", 3)
	v.SetDefault("audit.streamMaxLen", 100000)
	v.SetDefault("audit.dlqMaxRetries", 3)
	v.SetDefault("audit.region", "cn-beijing")
	v.SetDefault("log.level", "info")
	v.SetDefault("otel.exporterURL", "")
	v.SetDefault("otel.sampleRatio", 0.1)
	v.SetDefault("otel.metricsEnabled", true)
	v.SetDefault("otel.logsEnabled", true)
	v.SetDefault("otel.exportInterval", 15)
	v.SetDefault("anomaly.enabled", true)
	v.SetDefault("anomaly.mlEnabled", false)       // V5-M ML，默认关闭
	v.SetDefault("anomaly.mlThreshold", 0.6)         // 异常分数阈值
	v.SetDefault("anomaly.mlModelDir", "models/anomaly/")
	v.SetDefault("anomaly.mlTrainInterval", 24)      // 24 小时
	v.SetDefault("crypto.aesKey", "dev-aes-key-32-bytes-1234567890a") // 32 bytes
	// === V5-S: OAuth 2.1 + SCIM 默认值 ===
	v.SetDefault("oauth2.clientId", "ykt-aisaas")
	v.SetDefault("oauth2.clientSecret", "dev-oauth2-secret")
	v.SetDefault("oauth2.dpopEnabled", false) // DPoP disabled by default, enable in production
	v.SetDefault("scim.authToken", "dev-scim-token")
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
		"ykt_aisaas_anomaly_rules",
		"ykt_aisaas_anomaly_events",
		"ykt_aisaas_ml_models",
		"ykt_aisaas_ml_anomaly_rules",
	})
}
