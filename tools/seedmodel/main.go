// seed-model 工具：加密上游密钥并写入 model_registry（替换 REPLACE_VIA_SEAE 占位）。
//
// 用法：
//	go run ./tools/seedmodel -aes-key <32字节key> -dsn <mysql dsn> \
//	  -model demo-chat -base-url http://127.0.0.1:18080/v1 -upstream demo-chat -api-key demo-key
package main

import (
	"flag"
	"fmt"
	"os"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	pcrypto "ykt.dev/aisaas/internal/platform/crypto"
)

func main() {
	aesKey := flag.String("aes-key", "dev-aes-key-32-bytes-1234567890a", "32-byte AES key")
	dsn := flag.String("dsn", "root:root@tcp(127.0.0.1:13306)/ykt_aisaas?charset=utf8mb4&parseTime=true&loc=Local", "mysql dsn")
	model := flag.String("model", "demo-chat", "platform model id")
	baseURL := flag.String("base-url", "http://127.0.0.1:18080/v1", "upstream base url")
	upstream := flag.String("upstream", "demo-chat", "upstream model name")
	apiKey := flag.String("api-key", "demo-key", "upstream api key (plaintext)")
	provider := flag.String("provider", "mock", "provider tag")
	modelType := flag.String("type", "chat", "model type: chat/embedding/tts/asr")
	rowID := flag.Int64("id", 1, "registry row id")
	flag.Parse()

	enc, err := pcrypto.Encrypt([]byte(*apiKey), []byte(*aesKey))
	if err != nil {
		fmt.Fprintln(os.Stderr, "encrypt:", err)
		os.Exit(1)
	}

	db, err := gorm.Open(mysql.Open(*dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "mysql:", err)
		os.Exit(1)
	}
	res := db.Exec(`UPDATE ykt_aisaas_model_registry
		SET baseUrl = ?, apiKeyEnc = ?, upstreamModel = ?, provider = ?, type = ?
		WHERE modelId = ?`, *baseURL, enc, *upstream, *provider, *modelType, *model)
	if res.Error != nil {
		fmt.Fprintln(os.Stderr, "update:", res.Error)
		os.Exit(1)
	}
	if res.RowsAffected == 0 {
		res = db.Exec(`INSERT INTO ykt_aisaas_model_registry
			(id, tenantId, modelId, provider, baseUrl, apiKeyEnc, upstreamModel, modality, type, contextLength, isDefault, status)
			VALUES (?, NULL, ?, ?, ?, ?, ?, '["text"]', ?, 8192, 0, 1)
			ON DUPLICATE KEY UPDATE apiKeyEnc = VALUES(apiKeyEnc)`,
			*rowID, *model, *provider, *baseURL, enc, *upstream, *modelType)
		if res.Error != nil {
			fmt.Fprintln(os.Stderr, "insert:", res.Error)
			os.Exit(1)
		}
	}
	fmt.Println("seeded model", *model, "->", *baseURL, "upstream:", *upstream)
}
