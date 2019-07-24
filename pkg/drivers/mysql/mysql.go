package mysql

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"

	"github.com/coreos/etcd/pkg/transport"
	"github.com/go-sql-driver/mysql"
	"github.com/rancher/kine/pkg/drivers/generic"
	"github.com/rancher/kine/pkg/logstructured"
	"github.com/rancher/kine/pkg/logstructured/sqllog"
	"github.com/rancher/kine/pkg/server"
)

const (
	defaultUnixDSN = "root@unix(/var/run/mysqld/mysqld.sock)/"
	defaultHostDSN = "root@tcp(127.0.0.1)/"
)

var (
	schema = []string{
		`create table if not exists key_value
			(
				id INTEGER AUTO_INCREMENT,
				name TEXT,
				created INTEGER,
				deleted INTEGER,
				create_revision INTEGER,
 				prev_revision INTEGER,
				lease INTEGER,
 				value BLOB,
 				old_value BLOB,
				PRIMARY KEY (id)
			);`,
	}
	nameIdx     = "create index key_value_name_index on key_value (name(100))"
	revisionIdx = "create index key_value_name_prev_revision_uindex on key_value (name(100), prev_revision)"
	createDB    = "create database if not exists "
)

func New(dataSourceName string, tlsInfo *transport.TLSInfo) (server.Backend, error) {
	tlsConfig, err := tlsInfo.ClientConfig()
	if err != nil {
		return nil, err
	}
	tlsConfig.MinVersion = tls.VersionTLS11
	if len(tlsInfo.CertFile) == 0 && len(tlsInfo.KeyFile) == 0 && len(tlsInfo.CAFile) == 0 {
		tlsConfig = nil
	}
	parsedDSN, err := prepareDSN(dataSourceName, tlsConfig)
	if err != nil {
		return nil, err
	}
	fmt.Println("here")
	if err := createDBIfNotExist(parsedDSN); err != nil {
		return nil, err
	}

	dialect, err := generic.Open("mysql", parsedDSN, "?", false)
	if err != nil {
		return nil, err
	}
	dialect.LastInsertID = true
	if err := setup(dialect.DB, parsedDSN, tlsInfo); err != nil {
		return nil, err
	}

	return logstructured.New(sqllog.New(dialect)), nil
}

func setup(db *sql.DB, parsedDSN string, tlsInfo *transport.TLSInfo) error {
	for _, stmt := range schema {
		_, err := db.Exec(stmt)
		if err != nil {
			return err
		}
	}
	// check if duplicate indexes
	indexes := []string{
		nameIdx,
		revisionIdx}

	for _, idx := range indexes {
		err := createIndex(db, idx)
		if err != nil {
			return err
		}
	}
	return nil
}

func createDBIfNotExist(dataSourceName string) error {
	config, err := mysql.ParseDSN(dataSourceName)
	if err != nil {
		return err
	}
	dbName := config.DBName

	db, err := sql.Open("mysql", dataSourceName)
	if err != nil {
		return err
	}
	_, err = db.Exec(createDB + dbName)
	if err != nil {
		if mysqlError, ok := err.(*mysql.MySQLError); !ok || mysqlError.Number != 1049 {
			return err
		}
		config.DBName = ""
		db, err = sql.Open("mysql", config.FormatDSN())
		if err != nil {
			return err
		}
		_, err = db.Exec(createDB + dbName)
		if err != nil {
			return err
		}
	}
	return nil
}

func q(sql string) string {
	regex := regexp.MustCompile(`\?`)
	pref := "$"
	n := 0
	return regex.ReplaceAllStringFunc(sql, func(string) string {
		n++
		return pref + strconv.Itoa(n)
	})
}

func prepareDSN(dataSourceName string, tlsConfig *tls.Config) (string, error) {
	if len(dataSourceName) == 0 {
		dataSourceName = defaultUnixDSN
		if tlsConfig != nil {
			dataSourceName = defaultHostDSN
		}
	}
	config, err := mysql.ParseDSN(dataSourceName)
	if err != nil {
		return "", err
	}
	// setting up tlsConfig
	if tlsConfig != nil {
		mysql.RegisterTLSConfig("custom", tlsConfig)
		config.TLSConfig = "custom"
	}
	dbName := "kubernetes"
	if len(config.DBName) > 0 {
		dbName = config.DBName
	}
	config.DBName = dbName
	parsedDSN := config.FormatDSN()

	return parsedDSN, nil
}

func createIndex(db *sql.DB, indexStmt string) error {
	_, err := db.Exec(indexStmt)
	if err != nil {
		if mysqlError, ok := err.(*mysql.MySQLError); !ok || mysqlError.Number != 1061 {
			return err
		}
	}
	return nil
}
