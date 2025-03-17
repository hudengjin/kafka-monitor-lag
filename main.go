package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/hudengjin/kafka-monitor-lag/storage"
	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Logging struct {
		Level      string `yaml:"level"`
		Format     string `yaml:"format"`
		OutputPath string `yaml:"output_path"`
		Rotation   struct {
			MaxSize    int  `yaml:"max_size"`
			MaxBackups int  `yaml:"max_backups"`
			MaxAge     int  `yaml:"max_age"`
			Compress   bool `yaml:"compress"`
		} `yaml:"rotation"`
	} `yaml:"logging"`
	Kafka struct {
		Brokers []string `yaml:"brokers"`
		SASL    struct {
			Username  string `yaml:"username"`
			Password  string `yaml:"password"`
			Mechanism string `yaml:"mechanism"`
		} `yaml:"sasl"`
		Topics []struct {
			Name   string `yaml:"name"`
			Groups []struct {
				Name           string `yaml:"name"`
				AlertThreshold int64  `yaml:"alert_threshold"`
				IsNotify       bool   `yaml:"is_notify"`
			} `yaml:"groups"`
			NotifyList []string `yaml:"notify_list"`
		} `yaml:"topics"`
	} `yaml:"kafka"`

	Alert struct {
		APIURL     string `yaml:"api_url"`
		RetryTimes int    `yaml:"retry_times"`
		Timeout    string `yaml:"timeout"`
	} `yaml:"alert"`

	MySQL struct {
		Host     string `yaml:"host"`
		Database string `yaml:"database"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
		Table    string `yaml:"table"`
	} `yaml:"mysql"`

	Monitor struct {
		IntervalSeconds int `yaml:"interval_seconds"`
	} `yaml:"monitor"`
}

var mysqlStorage *storage.MySQLStorage

func main() {
	// 加载配置文件
	config, err := loadConfig("config.yaml")

	// 初始化MySQL存储
	mysqlStorage = storage.NewMySQLStorage(storage.MySQLConfig{
		Host:     config.MySQL.Host,
		Database: config.MySQL.Database,
		Username: config.MySQL.Username,
		Password: config.MySQL.Password,
		Table:    config.MySQL.Table,
	})
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 创建 Kafka 客户端
	client, err := createKafkaClient(config)
	if err != nil {
		fmt.Printf("Failed to create kafka client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// 初始化日志
	logger := initLogger(config)
	defer logger.Sync()

	// 定时执行监控
	ticker := time.NewTicker(time.Duration(config.Monitor.IntervalSeconds) * time.Second)
	defer ticker.Stop()

	// 处理退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-ticker.C:
			checkConsumerLag(client, config, logger)
		case <-sigChan:
			fmt.Println("\nExiting...")
			return
		}
	}
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func createKafkaClient(config *Config) (sarama.Client, error) {
	conf := sarama.NewConfig()

	// 配置 SASL 认证
	if config.Kafka.SASL.Username != "" {
		conf.Net.SASL.Enable = true
		conf.Net.SASL.User = config.Kafka.SASL.Username
		conf.Net.SASL.Password = config.Kafka.SASL.Password
		conf.Net.SASL.Mechanism = sarama.SASLMechanism(config.Kafka.SASL.Mechanism)
	}

	return sarama.NewClient(config.Kafka.Brokers, conf)
}

// 初始化日志组件
func initLogger(config *Config) *zap.Logger {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var encoder zapcore.Encoder
	if config.Logging.Format == "json" {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	// 设置日志输出
	var outputs []zapcore.WriteSyncer
	outputs = append(outputs, zapcore.AddSync(os.Stdout))

	if config.Logging.OutputPath != "" {
		// 确保日志目录存在
		if err := os.MkdirAll("./logs", 0755); err != nil {
			zap.L().Error("Failed to create logs directory", zap.Error(err))
		}

		// 配置日志轮转
		lumberjackLogger := &lumberjack.Logger{
			Filename:   config.Logging.OutputPath,
			MaxSize:    config.Logging.Rotation.MaxSize, // MB
			MaxBackups: config.Logging.Rotation.MaxBackups,
			MaxAge:     config.Logging.Rotation.MaxAge, // days
			Compress:   config.Logging.Rotation.Compress,
		}
		outputs = append(outputs, zapcore.AddSync(lumberjackLogger))
	}

	// 设置日志级别
	logLevel := zap.InfoLevel
	switch config.Logging.Level {
	case "debug":
		logLevel = zap.DebugLevel
	case "warn":
		logLevel = zap.WarnLevel
	case "error":
		logLevel = zap.ErrorLevel
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.NewMultiWriteSyncer(outputs...),
		logLevel,
	)

	return zap.New(core).With(zap.String("service", "kafka-monitor"))
}

type LagStats struct {
	Total int64
	Max   int64
	Min   int64
	Count int
	Topic string
	Group string
}

type AlertContent struct {
	Topic      string `json:"topic"`
	Group      string `json:"group"`
	CurrentLag int64  `json:"current_lag"`
	Threshold  int64  `json:"threshold"`
}

type AlertBody struct {
	ToUser  string       `json:"toUser"`
	MsgType string       `json:"msgType"`
	Content AlertContent `json:"content"`
}

func (s *LagStats) update(lag int64) {
	s.Total += lag
	if lag > s.Max || s.Max == 0 {
		s.Max = lag
	}
	if lag < s.Min || s.Min == 0 {
		s.Min = lag
	}
	s.Count++
}

func checkConsumerLag(client sarama.Client, config *Config, logger *zap.Logger) {
	for _, topic := range config.Kafka.Topics {
		latestOffsets, err := getLatestOffsets(client, topic.Name)
		if err != nil {
			logger.Error("获取最新offset失败",
				zap.String("topic", topic.Name),
				zap.Error(err))
			continue
		}

		for _, group := range topic.Groups {
			stats := &LagStats{
				Topic: topic.Name,
				Group: group.Name,
			}

			currentOffsets, err := getConsumerOffsets(client, group.Name, topic.Name)
			if err != nil {
				logger.Error("获取消费offset失败",
					zap.String("group", group.Name),
					zap.Error(err))
				continue
			}

			var maxLag int64
			for partition, offset := range currentOffsets {
				lag := latestOffsets[partition] - offset
				stats.update(lag)
				if lag > maxLag {
					maxLag = lag
				}

				// 记录分区级详细日志
				if lag > group.AlertThreshold {
					logger.Info("分区延迟超过阈值详情",
						zap.String("topic", stats.Topic),
						zap.String("group", stats.Group),
						zap.Int32("partition", partition),
						zap.Int64("lag", lag))
				}

			}

			// 触发阈值告警
			if stats.Total > group.AlertThreshold {
				logger.Warn("消费延迟超过阈值",
					zap.String("topic", stats.Topic),
					zap.String("group", stats.Group),
					zap.Int64("threshold", group.AlertThreshold),
					zap.Int64("current", stats.Total))

				if group.IsNotify {
					// 发送API告警
					go sendAlert(config, stats.Topic, stats.Group, stats.Total, group.AlertThreshold, topic.NotifyList, logger)
				}

			}

			batchNo := time.Now().Format("200601021504")
			// 存储到MySQL并记录汇总日志
			if stats.Count > 0 {
				record := storage.LagRecord{
					StatsDate:       time.Now().Format("2006-01-02"),
					BatchNo:         batchNo,
					Topic:           stats.Topic,
					ConsumerGroup:   stats.Group,
					TotalLag:        stats.Total,
					MaxLag:          stats.Max,
					CurrentLag:      stats.Max,
					AvgLag:          stats.Total / int64(stats.Count),
					PartitionCounts: stats.Count,
					IsAlert:         stats.Total > group.AlertThreshold,
					Threshold:       group.AlertThreshold,
					RecordTime:      time.Now().Format("2006-01-02 15:04:05"),
				}

				if err := mysqlStorage.InsertLagRecord(record); err != nil {
					logger.Error("存储监控数据失败",
						zap.String("topic", stats.Topic),
						zap.String("group", stats.Group),
						zap.Error(err))
				}

				avg := float64(stats.Total) / float64(stats.Count)
				logger.Info("消费延迟统计",
					zap.String("topic", stats.Topic),
					zap.String("group", stats.Group),
					zap.Int64("total_lag", stats.Total),
					zap.Int64("max_lag", stats.Max),
					zap.Int64("min_lag", stats.Min),
					zap.Float64("avg_lag", avg),
					zap.Int("partitions", stats.Count))
			}
		}
	}
}

func getLatestOffsets(client sarama.Client, topic string) (map[int32]int64, error) {
	partitions, err := client.Partitions(topic)
	if err != nil {
		return nil, err
	}

	offsets := make(map[int32]int64)
	for _, partition := range partitions {
		offset, err := client.GetOffset(topic, partition, sarama.OffsetNewest)
		if err != nil {
			return nil, err
		}
		offsets[partition] = offset
	}
	return offsets, nil
}

// 解析持续时间字符串（例如 "5s"）
func parseDuration(durationStr string) time.Duration {
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return 5 * time.Second // 默认超时时间
	}
	return duration
}

// 发送告警到API
func sendAlert(config *Config, topic string, group string, currentLag int64, threshold int64, notifyList []string, logger *zap.Logger) {

	alertBody := AlertBody{
		ToUser:  strings.Join(notifyList, "|"),
		MsgType: "text",
		Content: AlertContent{
			Topic:      topic,
			Group:      group,
			CurrentLag: currentLag,
			Threshold:  threshold,
		},
	}

	jsonBody, _ := json.Marshal(alertBody)
	client := &http.Client{
		Timeout: parseDuration(config.Alert.Timeout),
	}

	for i := 0; i < config.Alert.RetryTimes; i++ {
		req, _ := http.NewRequest("POST", config.Alert.APIURL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		logger.Error("消息发送失败：", zap.Error(err))
		time.Sleep(time.Duration(i+1) * time.Second) // 指数退避
	}
}

func getConsumerOffsets(client sarama.Client, group, topic string) (map[int32]int64, error) {
	manager, err := sarama.NewOffsetManagerFromClient(group, client)
	if err != nil {
		return nil, err
	}
	defer manager.Close()

	partitions, err := client.Partitions(topic)
	if err != nil {
		return nil, err
	}

	offsets := make(map[int32]int64)
	for _, partition := range partitions {
		pom, err := manager.ManagePartition(topic, partition)
		if err != nil {
			return nil, err
		}
		defer pom.Close()

		offset, _ := pom.NextOffset()
		offsets[partition] = offset
	}
	return offsets, nil
}
