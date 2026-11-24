package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	commonServer "code-common/backend/server"
	"code-markdocx/cli"
	"code-markdocx/config"
	"code-markdocx/pkg/vlm"
	"code-markdocx/server"

	"github.com/gin-gonic/gin"
)

//go:embed all:frontend/dist
var frontendFS embed.FS

var (
	Version   = "dev"
	CommitID  = "unknown"
	BuildTime = "unknown"
)

func securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("X-Frame-Options", "SAMEORIGIN")
		c.Writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Next()
	}
}

func main() {
	config.AppVersion = Version

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "convert":
			os.Exit(cli.RunConvert(os.Args[2:]))
		case "version":
			fmt.Printf("%s Version=%s Commit=%s BuildTime=%s\n", config.ServiceName, Version, CommitID, BuildTime)
			return
		case "serve":
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		}
	}

	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	log.Printf("[%s] 启动无状态文档转换子服务 (Version=%s, Commit=%s, BuildTime=%s)",
		config.ServiceName, Version, CommitID, BuildTime)

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("[%s] 加载配置失败: %v", config.ServiceName, err)
	}
	if cfg.VLM.IsConfigured() {
		log.Printf("[%s] VLM 已配置: model=%s, base_url=%s, concurrency=%d",
			config.ServiceName, cfg.VLM.Model, cfg.VLM.BaseURL, cfg.VLM.MaxConcurrency)
	} else {
		log.Printf("[%s] 提示: config.yaml 尚未配置有效 VLM 参数（仅文本与占位提取可用，修改配置后需重启生效）", config.ServiceName)
	}

	store, err := server.NewJobStore(
		cfg.Storage.TempDir,
		cfg.Storage.JobTTLMinutes,
		cfg.Storage.MaxActiveJobs,
		cfg.Convert.RefDocMaxBytes,
	)
	if err != nil {
		log.Fatalf("[%s] 初始化临时任务存储失败: %v", config.ServiceName, err)
	}

	vlmClient := vlm.NewClient(vlm.ClientConfig{
		BaseURL:           cfg.VLM.BaseURL,
		APIKey:            cfg.VLM.APIKey,
		Model:             cfg.VLM.Model,
		Timeout:           time.Duration(cfg.VLM.TimeoutSeconds) * time.Second,
		MaxRetries:        cfg.VLM.MaxRetries,
		Temperature:       cfg.VLM.Temperature,
		MaxTokens:         cfg.VLM.MaxTokens,
		UserAgent:         cfg.VLM.EffectiveUserAgent(),
		ExtraSystemPrompt: cfg.VLM.ExtraSystemPrompt,
	})
	h := server.NewHandlers(cfg, store, vlmClient)

	err = commonServer.Run(commonServer.Options{
		ServiceName:       config.ServiceName,
		Prefix:            config.ModuleKey,
		Port:              cfg.Server.Port,
		GinLog:            cfg.Server.GinLog,
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		MaxHeaderBytes:    cfg.Server.MaxHeaderBytes,
		FrontendFS:        &frontendFS,
		FrontendDistPath:  "frontend/dist",
		CustomMiddlewares: []gin.HandlerFunc{
			securityHeadersMiddleware(),
			gin.CustomRecovery(func(c *gin.Context, recovered any) {
				if recovered == http.ErrAbortHandler {
					c.Abort()
					return
				}
				errStr := fmt.Sprintf("%v", recovered)
				if strings.Contains(errStr, "net/http: abort Handler") || strings.Contains(errStr, "broken pipe") {
					c.Abort()
					return
				}
				log.Printf("[Recovery] panic recovered: %v", recovered)
				c.AbortWithStatus(http.StatusInternalServerError)
			}),
		},
		OnShutdown: func(ctx context.Context) {
			log.Printf("[%s] 收到停机信号，正在清理临时任务目录...", config.ServiceName)
			store.Close()
		},
		RegisterRoutes: func(r *gin.Engine) {
			server.RegisterRoutes(r, h)
		},
	})
	if err != nil {
		log.Fatalf("[%s] 服务运行异常退出: %v", config.ServiceName, err)
	}
}
