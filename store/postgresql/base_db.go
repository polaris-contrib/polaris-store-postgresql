package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/polarismesh/polaris/common/log"
	"github.com/polarismesh/polaris/plugin"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// db抛出的异常，需要重试的字符串组
var errMsg = []string{"Deadlock", "bad connection", "invalid connection"}

// BaseDB 对sql.DB的封装
type BaseDB struct {
	*sql.DB
	cfg      *dbConfig
	parsePwd plugin.ParsePassword
}

// dbConfig store的配置
type dbConfig struct {
	dbType          string
	dbUser          string
	dbPwd           string
	dbAddr          string
	dbPort          string
	dbName          string
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime int
}

// NewBaseDB 新建一个BaseDB
func NewBaseDB(cfg *dbConfig, parsePwd plugin.ParsePassword) (*BaseDB, error) {
	baseDb := &BaseDB{cfg: cfg, parsePwd: parsePwd}

	if err := baseDb.openDatabase(); err != nil {
		return nil, err
	}

	return baseDb, nil
}

// openDatabase 与数据库进行连接
func (b *BaseDB) openDatabase() error {
	c := b.cfg

	// 密码解析插件
	if b.parsePwd != nil {
		pwd, err := b.parsePwd.ParsePassword(c.dbPwd)
		if err != nil {
			log.Errorf("[Store][database][ParsePwdPlugin] parse password err: %s", err.Error())
			return err
		}
		c.dbPwd = pwd
	}

	dns := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", c.dbAddr, c.dbPort, c.dbUser, c.dbPwd, c.dbName)
	db, err := sql.Open(c.dbType, dns)
	if err != nil {
		log.Errorf("[Store][database] sql open err: %s", err.Error())
		return err
	}
	if pingErr := db.Ping(); pingErr != nil {
		log.Errorf("[Store][database] database ping err: %s", pingErr.Error())
		return pingErr
	}
	if c.maxOpenConns > 0 {
		log.Infof("[Store][database] db set max open conns: %d", c.maxOpenConns)
		db.SetMaxOpenConns(c.maxOpenConns)
	}
	if c.maxIdleConns > 0 {
		log.Infof("[Store][database] db set max idle conns: %d", c.maxIdleConns)
		db.SetMaxIdleConns(c.maxIdleConns)
	}
	if c.connMaxLifetime > 0 {
		log.Infof("[Store][database] db set conn max life time: %d", c.connMaxLifetime)
		db.SetConnMaxLifetime(time.Second * time.Duration(c.connMaxLifetime))
	}

	b.DB = db

	return nil
}

// Exec 重写db.Exec函数 提供重试功能
func (b *BaseDB) Exec(query string, args ...interface{}) (sql.Result, error) {
	var (
		result sql.Result
		err    error
	)

	Retry("exec "+query, func() error {
		result, err = b.DB.Exec(query, args...)
		return err
	})

	return result, err
}

// Query 重写db.Query函数
func (b *BaseDB) Query(query string, args ...interface{}) (*sql.Rows, error) {
	var (
		rows *sql.Rows
		err  error
	)

	Retry("query "+query, func() error {
		rows, err = b.DB.Query(query, args...)
		return err
	})

	return rows, err
}

// Begin 重写db.Begin
func (b *BaseDB) Begin() (*BaseTx, error) {
	var (
		tx     *sql.Tx
		err    error
		option *sql.TxOptions
	)

	Retry("begin", func() error {
		tx, err = b.DB.BeginTx(context.Background(), option)
		return err
	})

	return &BaseTx{Tx: tx}, err
}

// BaseTx 对sql.Tx的封装
type BaseTx struct {
	*sql.Tx
}

// Retry 重试主函数
// 最多重试20次，每次等待5ms*重试次数
func Retry(label string, handle func() error) {
	var (
		err         error
		maxTryTimes = 20
	)

	for i := 1; i <= maxTryTimes; i++ {
		err = handle()
		if err == nil {
			return
		}

		// 是否重试
		repeated := false
		for _, msg := range errMsg {
			if strings.Contains(err.Error(), msg) {
				log.Warnf("[Store][database][%s] get error msg: %s. Repeated doing(%d)", label, err.Error(), i)
				time.Sleep(time.Millisecond * 5 * time.Duration(i))
				repeated = true
				break
			}
		}
		if !repeated {
			return
		}
	}
}

// RetryTransaction 事务重试
func RetryTransaction(label string, handle func() error) error {
	var err error

	Retry(label, func() error {
		err = handle()
		return err
	})

	return err
}

func (b *BaseDB) processWithTransaction(label string, handle func(tx *BaseTx) error) error {
	tx, err := b.Begin()
	if err != nil {
		log.Errorf("[Store][database] %s begin tx err: %s", label, err.Error())
		return err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	return handle(tx)
}
