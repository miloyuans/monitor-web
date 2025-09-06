package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// AlertEvent represents the structure of incoming alert events from monitor-service
type AlertEvent struct {
	Timestamp       time.Time `json:"timestamp"`
	Module          string    `json:"module"`
	ServiceName     string    `json:"service_name"`
	EventName       string    `json:"event_name"`
	Details         string    `json:"details"`
	HostIP          string    `json:"host_ip"`
	AlertType       string    `json:"alert_type"`
	ClusterName     string    `json:"cluster_name"`
	Hostname        string    `json:"hostname"`
	BigKeysCount    *int      `json:"big_keys_count,omitempty"`      // Redis-specific
	FailedNodes     *string   `json:"failed_nodes,omitempty"`        // Redis-specific
	DeadlocksInc    *int64    `json:"deadlocks_increment,omitempty"` // MySQL-specific
	SlowQueriesInc  *int64    `json:"slow_queries_increment,omitempty"`
	Connections     *int      `json:"connections,omitempty"`
	CPUUsage        *float64  `json:"cpu_usage,omitempty"`     // Host-specific
	MemRemaining    *float64  `json:"mem_remaining,omitempty"`
	DiskUsage       *float64  `json:"disk_usage,omitempty"`
	AddedUsers      *string   `json:"added_users,omitempty"`    // System-specific
	RemovedUsers    *string   `json:"removed_users,omitempty"`
	AddedProcesses  *string   `json:"added_processes,omitempty"`
	RemovedProcesses *string  `json:"removed_processes,omitempty"`
}

// Alert is the general alerts table model
type Alert struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement"`
	Timestamp   time.Time `gorm:"index;not null"`
	Module      string    `gorm:"index;not null;size:50"`
	ServiceName string    `gorm:"not null;size:100"`
	EventName   string    `gorm:"not null;size:100"`
	Details     string    `gorm:"not null;type:text"`
	HostIP      string    `gorm:"not null;size:50"`
	AlertType   string    `gorm:"not null;size:50"`
	ClusterName string    `gorm:"not null;size:100"`
	Hostname    string    `gorm:"not null;size:100"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
}

// RedisAlert is the Redis-specific alerts table model
type RedisAlert struct {
	Alert
	BigKeysCount int    `gorm:"default:0"`
	FailedNodes  string `gorm:"type:text"`
}

// MySQLAlert is the MySQL-specific alerts table model
type MySQLAlert struct {
	Alert
	DeadlocksIncrement  int64 `gorm:"default:0"`
	SlowQueriesIncrement int64 `gorm:"default:0"`
	Connections         int   `gorm:"default:0"`
}

// HostAlert is the Host-specific alerts table model
type HostAlert struct {
	Alert
	CPUUsage     float64 `gorm:"default:0"`
	MemRemaining float64 `gorm:"default:0"`
	DiskUsage    float64 `gorm:"default:0"`
}

// SystemAlert is the System-specific alerts table model
type SystemAlert struct {
	Alert
	AddedUsers      string `gorm:"type:text"`
	RemovedUsers    string `gorm:"type:text"`
	AddedProcesses   string `gorm:"type:text"`
	RemovedProcesses string `gorm:"type:text"`
}

var db *gorm.DB

func main() {
	// Initialize logger
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Load environment variables
	if err := initConfig(); err != nil {
		slog.Error("Failed to load configuration", "error", err, "component", "monitor-web")
		os.Exit(1)
	}

	// Initialize database
	var err error
	db, err = initDB()
	if err != nil {
		slog.Error("Failed to connect to database", "error", err, "component", "monitor-web")
		os.Exit(1)
	}

	// Auto-migrate tables
	if err := db.AutoMigrate(&Alert{}, &RedisAlert{}, &MySQLAlert{}, &HostAlert{}, &SystemAlert{}); err != nil {
		slog.Error("Failed to auto-migrate tables", "error", err, "component", "monitor-web")
		os.Exit(1)
	}
	slog.Info("Database tables migrated successfully", "component", "monitor-web")

	// Initialize Gin router
	r := gin.Default()

	// Serve static files (Chart.js, CSS, etc.)
	r.Static("/static", "./static")

	// Load HTML templates (assuming templates are in ./templates)
	r.LoadHTMLGlob("templates/*")

	// Routes
	r.POST("/api/alerts", receiveAlert)
	r.GET("/dashboard/:module", showDashboard)

	// Start server
	port := viper.GetString("WEB_PORT")
	if port == "" {
		port = "8080"
	}
	slog.Info("Starting web server", "port", port, "component", "monitor-web")
	if err := r.Run(":" + port); err != nil {
		slog.Error("Failed to start web server", "error", err, "port", port, "component", "monitor-web")
		os.Exit(1)
	}
}

// initConfig loads configuration from environment variables
func initConfig() error {
	viper.SetEnvPrefix("MONITOR_WEB")
	viper.AutomaticEnv()

	// Required environment variables
	viper.SetDefault("DB_HOST", "localhost")
	viper.SetDefault("DB_PORT", "3306")
	viper.SetDefault("DB_NAME", "monitor_db")
	viper.SetDefault("DB_USER", "root")
	viper.SetDefault("DB_PASS", "")
	viper.SetDefault("WEB_PORT", "8080")

	// Validate required fields
	if viper.GetString("DB_NAME") == "" {
		return fmt.Errorf("MONITOR_WEB_DB_NAME is required")
	}
	if viper.GetString("DB_USER") == "" {
		return fmt.Errorf("MONITOR_WEB_DB_USER is required")
	}
	return nil
}

// initDB initializes the MySQL database connection using environment variables
func initDB() (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		viper.GetString("DB_USER"),
		viper.GetString("DB_PASS"),
		viper.GetString("DB_HOST"),
		viper.GetString("DB_PORT"),
		viper.GetString("DB_NAME"),
	)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true, // Avoid FK issues during migration
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MySQL: %w", err)
	}
	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)
	return db, nil
}

// receiveAlert handles incoming alert events and stores them in the database
func receiveAlert(c *gin.Context) {
	var event AlertEvent
	if err := c.ShouldBindJSON(&event); err != nil {
		slog.Error("Failed to parse alert JSON", "error", err, "component", "monitor-web")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	// Validate required fields
	if event.Module == "" || event.ServiceName == "" || event.EventName == "" {
		slog.Error("Missing required fields in alert", "module", event.Module, "component", "monitor-web")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required fields"})
		return
	}

	// Common alert fields
	alert := Alert{
		Timestamp:   event.Timestamp,
		Module:      event.Module,
		ServiceName: event.ServiceName,
		EventName:   event.EventName,
		Details:     event.Details,
		HostIP:      event.HostIP,
		AlertType:   event.AlertType,
		ClusterName: event.ClusterName,
		Hostname:    event.Hostname,
	}

	// Store in module-specific table
	tx := db.Begin()
	switch event.Module {
	case "redis":
		redisAlert := RedisAlert{
			Alert:       alert,
			BigKeysCount: 0,
			FailedNodes:  "",
		}
		if event.BigKeysCount != nil {
			redisAlert.BigKeysCount = *event.BigKeysCount
		}
		if event.FailedNodes != nil {
			redisAlert.FailedNodes = *event.FailedNodes
		}
		if err := tx.Create(&redisAlert).Error; err != nil {
			tx.Rollback()
			slog.Error("Failed to store redis alert", "error", err, "component", "monitor-web")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store alert"})
			return
		}
	case "mysql":
		mysqlAlert := MySQLAlert{
			Alert:                alert,
			DeadlocksIncrement:   0,
			SlowQueriesIncrement: 0,
			Connections:          0,
		}
		if event.DeadlocksInc != nil {
			mysqlAlert.DeadlocksIncrement = *event.DeadlocksInc
		}
		if event.SlowQueriesInc != nil {
			mysqlAlert.SlowQueriesIncrement = *event.SlowQueriesInc
		}
		if event.Connections != nil {
			mysqlAlert.Connections = *event.Connections
		}
		if err := tx.Create(&mysqlAlert).Error; err != nil {
			tx.Rollback()
			slog.Error("Failed to store mysql alert", "error", err, "component", "monitor-web")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store alert"})
			return
		}
	case "host":
		hostAlert := HostAlert{
			Alert:        alert,
			CPUUsage:     0,
			MemRemaining: 0,
			DiskUsage:    0,
		}
		if event.CPUUsage != nil {
			hostAlert.CPUUsage = *event.CPUUsage
		}
		if event.MemRemaining != nil {
			hostAlert.MemRemaining = *event.MemRemaining
		}
		if event.DiskUsage != nil {
			hostAlert.DiskUsage = *event.DiskUsage
		}
		if err := tx.Create(&hostAlert).Error; err != nil {
			tx.Rollback()
			slog.Error("Failed to store host alert", "error", err, "component", "monitor-web")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store alert"})
			return
		}
	case "system":
		systemAlert := SystemAlert{
			Alert:           alert,
			AddedUsers:      "",
			RemovedUsers:    "",
			AddedProcesses:   "",
			RemovedProcesses: "",
		}
		if event.AddedUsers != nil {
			systemAlert.AddedUsers = *event.AddedUsers
		}
		if event.RemovedUsers != nil {
			systemAlert.RemovedUsers = *event.RemovedUsers
		}
		if event.AddedProcesses != nil {
			systemAlert.AddedProcesses = *event.AddedProcesses
		}
		if event.RemovedProcesses != nil {
			systemAlert.RemovedProcesses = *event.RemovedProcesses
		}
		if err := tx.Create(&systemAlert).Error; err != nil {
			tx.Rollback()
			slog.Error("Failed to store system alert", "error", err, "component", "monitor-web")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store alert"})
			return
		}
	default:
		// Fallback to general alerts table
		if err := tx.Create(&alert).Error; err != nil {
			tx.Rollback()
			slog.Error("Failed to store general alert", "error", err, "component", "monitor-web")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store alert"})
			return
		}
	}
	tx.Commit()
	slog.Info("Stored alert", "module", event.Module, "event_name", event.EventName, "component", "monitor-web")
	c.JSON(http.StatusOK, gin.H{"status": "stored"})
}

// showDashboard renders the dashboard for a specific module
func showDashboard(c *gin.Context) {
	module := c.Param("module")
	var alerts []map[string]interface{}
	tableName := module + "_alerts"

	// Validate module
	validModules := []string{"redis", "mysql", "host", "system", "general", "rabbitmq", "nacos"}
	isValid := false
	for _, m := range validModules {
		if module == m {
			isValid = true
			break
		}
	}
	if !isValid {
		slog.Warn("Invalid module requested", "module", module, "component", "monitor-web")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid module"})
		return
	}

	// Query parameters for filtering
	from := c.Query("from") // e.g., 2025-09-01
	to := c.Query("to")     // e.g., 2025-09-06
	alertType := c.Query("alert_type")

	query := db.Table(tableName).Order("timestamp desc").Limit(100)
	if from != "" {
		if t, err := time.Parse("2006-01-02", from); err == nil {
			query = query.Where("timestamp >= ?", t)
		}
	}
	if to != "" {
		if t, err := time.Parse("2006-01-02", to); err == nil {
			query = query.Where("timestamp <= ?", t)
		}
	}
	if alertType != "" {
		query = query.Where("alert_type = ?", alertType)
	}

	if err := query.Find(&alerts).Error; err != nil {
		slog.Error("Failed to query alerts", "module", module, "error", err, "component", "monitor-web")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query alerts"})
		return
	}

	// Prepare chart data (for Chart.js)
	chartData := map[string]interface{}{
		"labels": []string{},
		"datasets": []map[string]interface{}{
			{
				"label":           "Alert Count",
				"data":            []int{},
				"borderColor":     "#3b82f6",
				"backgroundColor": "#3b82f6",
				"fill":            false,
			},
		},
	}
	// Aggregate alerts by day for chart
	dayCounts := make(map[string]int)
	for _, alert := range alerts {
		ts, ok := alert["timestamp"].(time.Time)
		if !ok {
			continue
		}
		day := ts.Format("2006-01-02")
		dayCounts[day]++
	}
	var days []string
	for day := range dayCounts {
		days = append(days, day)
	}
	sort.Strings(days)
	for _, day := range days {
		chartData["labels"] = append(chartData["labels"], day)
		chartData["datasets"].( []map[string]interface{})[0]["data"] = append(chartData["datasets"].( []map[string]interface{})[0]["data"], dayCounts[day])
	}

	// Render template
	c.HTML(http.StatusOK, "dashboard.tmpl", gin.H{
		"Module":    module,
		"Alerts":    alerts,
		"ChartData": chartData,
	})
}