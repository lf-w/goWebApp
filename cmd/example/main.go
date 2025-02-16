package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"ibingli.com/internal/app/example/echoargs/echoargsctl"
	"ibingli.com/internal/app/example/useraccount/useraccountctl"
	"ibingli.com/internal/app/example/useraccount/useraccountsrv"
	"ibingli.com/internal/pkg/myConfig/viperImp"
	"ibingli.com/internal/pkg/myHttpServer/myHttpServerImp"
	"ibingli.com/internal/pkg/myLog/zapImp"

	"github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
)

var (
	branchName string
	commitId   string
	buildTime  string
)

func main() {
	showVer := flag.Bool("v", false, "show version")
	profile := flag.String("profile", "example", "Environment profile, something similar to spring profile")
	flag.Parse()
	if *showVer {
		fmt.Printf("%s: %s\t%s\n", branchName, commitId, buildTime)
		os.Exit(0)
	}
	fmt.Printf("using profile: %s\n", *profile)

	config, err := viperImp.New(*profile)
	if err != nil {
		log.Fatalf("读取配置信息失败, err: %s", err)
	}

	logger, err := zapImp.New(zapImp.Configuration{
		EnableConsole:     config.GetBoolOrDefault("log.enableConsole", true),
		ConsoleJSONFormat: config.GetBoolOrDefault("log.consoleJSONFormat", false),
		ConsoleLevel:      config.GetStringOrDefault("log.consoleLevel", "debug"),
		EnableFile:        config.GetBoolOrDefault("log.enableFile", false),
		FileJSONFormat:    config.GetBoolOrDefault("log.fileJSONFormat", false),
		FileLevel:         config.GetStringOrDefault("log.fileLevel", "info"),
		FileLocation:      config.GetStringOrDefault("log.fileLocation", "/tmp/goapp/example_info.log"),
		ErrFileLevel:      config.GetStringOrDefault("log.errFileLevel", "error"),
		ErrFileLocation:   config.GetStringOrDefault("log.errFileLocation", "/tmp/goapp/example_err.log"),
	})
	if err != nil {
		logger.Fatalf("初始化日志模块失败, err: %s", err)
	}

	var db *sql.DB
	switch dbt := config.GetStringOrDefault("database.dialect", "mysql"); dbt {
	case "mysql":
		// mysql
		cfg := mysql.Config{
			User:                 config.GetStringOrDefault("database.user", ""), // os.Getenv("DBUSER"),
			Passwd:               config.GetStringOrDefault("database.pass", ""), // os.Getenv("DBPASS"),
			Net:                  "tcp",
			Addr:                 fmt.Sprintf("%s:%s", config.GetStringOrDefault("database.host", ""), config.GetStringOrDefault("database.port", "")),
			DBName:               config.GetStringOrDefault("database.db", ""),
			AllowNativePasswords: true,
			ParseTime:            true,
			Loc:                  time.Local,
		}
		db, err = sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			logger.Fatalf("初始化数据库连接失败, err: %s", err)
		}
		pingErr := db.Ping()
		if pingErr != nil {
			logger.Fatalf("%s", pingErr)
		}
		logger.Infof("数据库已连接!")
	case "sqlite":
		// sqlite
		dbPath := config.GetStringOrDefault("database.db", "/tmp/exam.sqlitedb")
		db, err = sql.Open("sqlite3", dbPath)
		if err != nil {
			// This will not be a connection error, but a DSN parse error or
			// another initialization error.
			logger.Fatalf("%s", err)
		}
		defer func() {
			os.Remove(dbPath)
		}()
		pingErr := db.Ping()
		if pingErr != nil {
			logger.Fatalf("%s", pingErr)
		}
		logger.Infof("数据库已连接!")
	default:
		logger.Fatalf("初始化数据库失败, 未知的数据库类型: %s", dbt)
	}
	db.SetConnMaxLifetime(time.Duration(config.GetIntOrDefault("database.connMaxLifeTimeSec", 600) * int(time.Second)))
	db.SetMaxIdleConns(config.GetIntOrDefault("database.maxIdleConn", 9))
	db.SetMaxOpenConns(config.GetIntOrDefault("database.maxOpenConn", 9))

	httpSrv := myHttpServerImp.New(&myHttpServerImp.Options{
		Port:        config.GetIntOrDefault("server.port", 0),
		Logger:      logger,
		AuthHandler: useraccountsrv.GetInstance(),
	})
	httpSrv.AddRouter(useraccountctl.New(config, logger, db))
	httpSrv.AddRouter(echoargsctl.New(config, logger, db))
	logger.Infof("http server 端口: %d", config.GetIntOrDefault("server.port", 0))

	var wg sync.WaitGroup
	// 启动服务
	wg.Add(1)
	go func() {
		defer wg.Done()
		httpSrv.Listen()
	}()

	// 等候关闭信号
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c

	// 关闭服务
	httpSrv.Shutdown()
	// 等待服务完全关闭
	wg.Wait()
}
