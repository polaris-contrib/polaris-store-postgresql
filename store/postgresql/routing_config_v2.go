package postgresql

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/polarismesh/polaris/common/log"
	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/store"
	"go.uber.org/zap"
	"time"
)

var _ store.RoutingConfigStoreV2 = (*routingConfigStoreV2)(nil)

// RoutingConfigStoreV2 impl
type routingConfigStoreV2 struct {
	master *BaseDB
	slave  *BaseDB
}

// CreateRoutingConfigV2 Add a new routing configuration
func (r *routingConfigStoreV2) CreateRoutingConfigV2(conf *model.RouterConfig) error {
	if conf.ID == "" || conf.Revision == "" {
		log.Errorf("[Store][boltdb] create routing config v2 missing id or revision")
		return store.NewStatusError(store.EmptyParamsErr, "missing id or revision")
	}
	if conf.Policy == "" || conf.Config == "" {
		log.Errorf("[Store][boltdb] create routing config v2 missing params")
		return store.NewStatusError(store.EmptyParamsErr, "missing some params")
	}

	err := RetryTransaction("CreateRoutingConfigV2", func() error {
		tx, err := r.master.Begin()
		if err != nil {
			return err
		}

		defer func() {
			_ = tx.Rollback()
		}()
		if err := r.createRoutingConfigV2Tx(tx, conf); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			log.Errorf("[Store][database] create routing config v2(%+v) commit: %s", conf, err.Error())
			return store.Error(err)
		}

		return nil
	})

	return store.Error(err)
}

func (r *routingConfigStoreV2) CreateRoutingConfigV2Tx(tx store.Tx, conf *model.RouterConfig) error {
	if tx == nil {
		return errors.New("tx is nil")
	}

	dbTx := tx.GetDelegateTx().(*BaseTx)
	return r.createRoutingConfigV2Tx(dbTx, conf)
}

func (r *routingConfigStoreV2) createRoutingConfigV2Tx(tx *BaseTx, conf *model.RouterConfig) error {
	// 删除无效的数据
	stmt, err := tx.Prepare("DELETE FROM routing_config_v2 WHERE id = $1 AND flag = 1")
	if err != nil {
		return store.Error(err)
	}
	if _, err = stmt.Exec(conf.ID); err != nil {
		log.Errorf("[Store][database] create routing v2(%+v) err: %s", conf, err.Error())
		return store.Error(err)
	}

	insertSQL := "INSERT INTO routing_config_v2(id, namespace, name, policy, config, enable, " +
		" priority, revision, description, ctime, mtime, etime) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'%s')"

	var enable int
	if conf.Enable {
		enable = 1
		insertSQL = fmt.Sprintf(insertSQL, GetCurrentTimeFormat())
	} else {
		enable = 0
		insertSQL = fmt.Sprintf(insertSQL, emptyEnableTime)
	}

	log.Debug("[Store][database] create routing v2", zap.String("sql", insertSQL))

	stmt, err = tx.Prepare(insertSQL)
	if err != nil {
		return store.Error(err)
	}

	if _, err = stmt.Exec(conf.ID, conf.Namespace, conf.Name, conf.Policy,
		conf.Config, enable, conf.Priority, conf.Revision, conf.Description,
		GetCurrentTimeFormat(), GetCurrentTimeFormat()); err != nil {
		log.Errorf("[Store][database] create routing v2(%+v) err: %s", conf, err.Error())
		return store.Error(err)
	}
	return nil
}

// UpdateRoutingConfigV2 Update a routing configuration
func (r *routingConfigStoreV2) UpdateRoutingConfigV2(conf *model.RouterConfig) error {

	tx, err := r.master.Begin()
	if err != nil {
		return err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	if err := r.updateRoutingConfigV2Tx(tx, conf); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		log.Errorf("[Store][database] update routing config v2(%+v) commit: %s", conf, err.Error())
		return store.Error(err)
	}

	return nil
}

func (r *routingConfigStoreV2) UpdateRoutingConfigV2Tx(tx store.Tx, conf *model.RouterConfig) error {
	if tx == nil {
		return errors.New("tx is nil")
	}

	dbTx := tx.GetDelegateTx().(*BaseTx)
	return r.updateRoutingConfigV2Tx(dbTx, conf)
}

func (r *routingConfigStoreV2) updateRoutingConfigV2Tx(tx *BaseTx, conf *model.RouterConfig) error {
	if conf.ID == "" || conf.Revision == "" {
		log.Errorf("[Store][database] update routing config v2 missing id or revision")
		return store.NewStatusError(store.EmptyParamsErr, "missing id or revision")
	}
	if conf.Policy == "" || conf.Config == "" {
		log.Errorf("[Store][boltdb] create routing config v2 missing params")
		return store.NewStatusError(store.EmptyParamsErr, "missing some params")
	}

	str := "update routing_config_v2 set name = $1, policy = $2, config = $3, revision = $4, priority = $5, " +
		" description = $6, mtime = $7 where id = $8"
	stmt, err := tx.Prepare(str)
	if err != nil {
		return store.Error(err)
	}
	if _, err = stmt.Exec(conf.Name, conf.Policy, conf.Config, conf.Revision, conf.Priority,
		conf.Description, GetCurrentTimeFormat(), conf.ID); err != nil {
		log.Errorf("[Store][database] update routing config v2(%+v) exec err: %s", conf, err.Error())
		return store.Error(err)
	}
	return nil
}

// EnableRateLimit Enable current limit rules
func (r *routingConfigStoreV2) EnableRouting(conf *model.RouterConfig) error {
	if conf.ID == "" || conf.Revision == "" {
		return errors.New("[Store][database] enable routing config v2 missing some params")
	}

	err := RetryTransaction("EnableRouting", func() error {
		var (
			enable   int
			etimeStr string
		)
		if conf.Enable {
			enable = 1
			etimeStr = GetCurrentTimeFormat()
		} else {
			enable = 0
			etimeStr = emptyEnableTime
		}

		str := `update routing_config_v2 set enable = $1, revision = $2, mtime = $3, etime=$4 where id = $5`
		stmt, err := r.master.Prepare(str)
		if err != nil {
			return err
		}
		if _, err = stmt.Exec(enable, conf.Revision, GetCurrentTimeFormat(), etimeStr, conf.ID); err != nil {
			log.Errorf("[Store][database] update outing config v2(%+v), sql %s, err: %s", conf, str, err)
			return err
		}

		return nil
	})

	return store.Error(err)
}

// DeleteRoutingConfigV2 Delete a routing configuration
func (r *routingConfigStoreV2) DeleteRoutingConfigV2(ruleID string) error {

	if ruleID == "" {
		log.Errorf("[Store][database] delete routing config v2 missing service id")
		return store.NewStatusError(store.EmptyParamsErr, "missing service id")
	}

	str := `update routing_config_v2 set flag = 1, mtime = $1 where id = $2`
	stmt, err := r.master.Prepare(str)
	if err != nil {
		return store.Error(err)
	}
	if _, err = stmt.Exec(GetCurrentTimeFormat(), ruleID); err != nil {
		log.Errorf("[Store][database] delete routing config v2(%s) err: %s", ruleID, err.Error())
		return store.Error(err)
	}

	return nil
}

// GetRoutingConfigsV2ForCache Pull the incremental routing configuration information through mtime
func (r *routingConfigStoreV2) GetRoutingConfigsV2ForCache(
	mtime time.Time, firstUpdate bool) ([]*model.RouterConfig, error) {
	str := `select id, name, policy, config, enable, revision, flag, 
       priority, description, ctime, mtime, etime  
	from routing_config_v2 where mtime > $1 `

	if firstUpdate {
		str += " and flag != 1"
	}
	rows, err := r.slave.Query(str, mtime)
	if err != nil {
		log.Errorf("[Store][database] query routing configs v2 with mtime err: %s", err.Error())
		return nil, err
	}
	out, err := fetchRoutingConfigV2Rows(rows)
	if err != nil {
		return nil, err
	}

	return out, nil
}

// GetRoutingConfigV2WithID Pull the routing configuration according to the rules ID
func (r *routingConfigStoreV2) GetRoutingConfigV2WithID(ruleID string) (*model.RouterConfig, error) {

	tx, err := r.master.Begin()
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = tx.Rollback()
	}()
	return r.getRoutingConfigV2WithIDTx(tx, ruleID)
}

// GetRoutingConfigV2WithIDTx Pull the routing configuration according to the rules ID
func (r *routingConfigStoreV2) GetRoutingConfigV2WithIDTx(tx store.Tx, ruleID string) (*model.RouterConfig, error) {

	if tx == nil {
		return nil, errors.New("transaction is nil")
	}

	dbTx := tx.GetDelegateTx().(*BaseTx)
	return r.getRoutingConfigV2WithIDTx(dbTx, ruleID)
}

func (r *routingConfigStoreV2) getRoutingConfigV2WithIDTx(tx *BaseTx, ruleID string) (*model.RouterConfig, error) {

	str := `select id, name, policy, config, enable, revision, flag, priority, 
       	description, ctime, mtime, etime
		from routing_config_v2 
		where id = $1 and flag = 0`
	rows, err := tx.Query(str, ruleID)
	if err != nil {
		log.Errorf("[Store][database] query routing v2 with id(%s) err: %s", ruleID, err.Error())
		return nil, err
	}

	out, err := fetchRoutingConfigV2Rows(rows)
	if err != nil {
		return nil, err
	}

	if len(out) == 0 {
		return nil, nil
	}

	return out[0], nil
}

// fetchRoutingConfigRows Read the data of the database and release ROWS
func fetchRoutingConfigV2Rows(rows *sql.Rows) ([]*model.RouterConfig, error) {
	defer rows.Close()
	var out []*model.RouterConfig
	for rows.Next() {
		var (
			entry        model.RouterConfig
			flag, enable int
		)

		err := rows.Scan(&entry.ID, &entry.Name, &entry.Policy, &entry.Config, &enable, &entry.Revision,
			&flag, &entry.Priority, &entry.Description, &entry.CreateTime, &entry.ModifyTime, &entry.EnableTime)
		if err != nil {
			log.Errorf("[database][store] fetch routing config v2 scan err: %s", err.Error())
			return nil, err
		}

		entry.Valid = true
		if flag == 1 {
			entry.Valid = false
		}
		entry.Enable = enable == 1

		out = append(out, &entry)
	}
	if err := rows.Err(); err != nil {
		log.Errorf("[database][store] fetch routing config v2 next err: %s", err.Error())
		return nil, err
	}

	return out, nil
}
