// Open: reads the app password from the env file, connects to the local MySQL database and pings it.

package resultsdb

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

const (
	DefaultEnvFile = "mysql/.env"
	address        = "127.0.0.1:3307"
	database       = "ship_detections"

	user = "app"

	passwordKey = "APP_DB_PASSWORD"
)

func Open(envFile string) (*sql.DB, error) {
	values, err := godotenv.Read(envFile)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", envFile, err)
	}
	password := values[passwordKey]
	if password == "" {
		return nil, fmt.Errorf("%s has no %s", envFile, passwordKey)
	}

	config := mysql.NewConfig()
	config.User = user
	config.Passwd = password
	config.Net = "tcp"
	config.Addr = address
	config.DBName = database
	config.ParseTime = true
	config.Loc = time.UTC
	config.TLSConfig = "preferred"
	config.Timeout = 5 * time.Second
	config.ReadTimeout = 30 * time.Second
	config.WriteTimeout = 30 * time.Second

	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting to MySQL on %s as %s: %w", address, user, err)
	}
	return db, nil
}
