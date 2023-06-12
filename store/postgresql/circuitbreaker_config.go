package postgresql

import (
	"database/sql"
	"fmt"
	"github.com/polarismesh/polaris/common/log"
	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/store"
	"time"
)

const (
	labelCreateCircuitBreakerRuleOld    = "createCircuitBreakerRuleOld"
	labelTagCircuitBreakerRuleOld       = "tagCircuitBreakerRuleOld"
	labelDeleteTagCircuitBreakerRuleOld = "deleteTagCircuitBreakerRuleOld"
	labelReleaseCircuitBreakerRuleOld   = "releaseCircuitBreakerRuleOld"
	labelUnbindCircuitBreakerRuleOld    = "unbindCircuitBreakerRuleOld"
	labelUpdateCircuitBreakerRuleOld    = "updateCircuitBreakerRuleOld"
	labelDeleteCircuitBreakerRuleOld    = "deleteCircuitBreakerRuleOld"
)

// circuitBreakerStore 的实现
type circuitBreakerStore struct {
	master *BaseDB
	slave  *BaseDB
}

// CreateCircuitBreaker 创建一个新的熔断规则
func (c *circuitBreakerStore) CreateCircuitBreaker(cb *model.CircuitBreaker) error {
	return c.master.processWithTransaction(labelCreateCircuitBreakerRuleOld, func(tx *BaseTx) error {
		if err := cleanCircuitBreaker(tx, cb.ID, cb.Version); err != nil {
			log.Errorf("[Store][circuitBreaker] clean master for circuit breaker(%s, %s) err: %s",
				cb.ID, cb.Version, err.Error())
			return store.Error(err)
		}

		str := `insert into circuitbreaker_rule
			(id, version, name, namespace, business, department, comment, inbounds, 
			outbounds, token, owner, revision, flag, ctime, mtime)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`
		stmt, err := tx.Prepare(str)
		if err != nil {
			return store.Error(err)
		}
		if _, err = stmt.Exec(cb.ID, cb.Version, cb.Name, cb.Namespace, cb.Business, cb.Department,
			cb.Comment, cb.Inbounds, cb.Outbounds, cb.Token, cb.Owner, cb.Revision, 0,
			GetCurrentTimeFormat(), GetCurrentTimeFormat()); err != nil {
			log.Errorf("[Store][circuitBreaker] create circuit breaker(%s, %s, %s) err: %s",
				cb.ID, cb.Name, cb.Version, err.Error())
			return store.Error(err)
		}
		if err := tx.Commit(); err != nil {
			log.Errorf("[Store][database] fail to %s commit tx, create rule(%+v) commit tx err: %s",
				labelCreateCircuitBreakerRuleOld, cb, err.Error())
			return err
		}
		return nil
	})
}

// TagCircuitBreaker 给master熔断规则打一个version tag
func (c *circuitBreakerStore) TagCircuitBreaker(cb *model.CircuitBreaker) error {
	return c.master.processWithTransaction(labelTagCircuitBreakerRuleOld, func(tx *BaseTx) error {
		if err := cleanCircuitBreaker(tx, cb.ID, cb.Version); err != nil {
			log.Errorf("[Store][circuitBreaker] clean tag for circuit breaker(%s, %s) err: %s",
				cb.ID, cb.Version, err.Error())
			return store.Error(err)
		}

		if err := tagCircuitBreaker(tx, cb); err != nil {
			log.Errorf("[Store][circuitBreaker] create tag for circuit breaker(%s, %s) err: %s",
				cb.ID, cb.Version, err.Error())
			return store.Error(err)
		}
		if err := tx.Commit(); err != nil {
			log.Errorf("[Store][database] fail to %s commit tx, tag rule(%+v) commit tx err: %s",
				labelTagCircuitBreakerRuleOld, cb, err.Error())
			return err
		}
		return nil
	})
}

// tagCircuitBreaker 给master熔断规则打一个version tag的内部函数
func tagCircuitBreaker(tx *BaseTx, cb *model.CircuitBreaker) error {
	// 需要保证master规则存在
	str := `insert into circuitbreaker_rule
			(id, version, name, namespace, business, department, comment, inbounds, 
			outbounds, token, owner, revision, ctime, mtime) 
			select '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', 
			'%s', '%s', '%s', '%s', '%s', '%s' from circuitbreaker_rule 
			where id = $1 and version = 'master'`
	str = fmt.Sprintf(str, cb.ID, cb.Version, cb.Name, cb.Namespace, cb.Business, cb.Department, cb.Comment,
		cb.Inbounds, cb.Outbounds, cb.Token, cb.Owner, cb.Revision, GetCurrentTimeFormat(), GetCurrentTimeFormat())
	stmt, err := tx.Prepare(str)
	if err != nil {
		return err
	}
	result, err := stmt.Exec(str, cb.ID)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] exec create tag sql(%s) err: %s", str, err.Error())
		return err
	}

	if err := checkDataBaseAffectedRows(result, 1); err != nil {
		if store.Code(err) == store.AffectedRowsNotMatch {
			return store.NewStatusError(store.NotFoundMasterConfig, "not found master config")
		}
		log.Errorf("[Store][CircuitBreaker] tag rule affected rows err: %s", err.Error())
		return err
	}

	return nil
}

// ReleaseCircuitBreaker 发布熔断规则
func (c *circuitBreakerStore) ReleaseCircuitBreaker(cbr *model.CircuitBreakerRelation) error {
	return c.master.processWithTransaction(labelReleaseCircuitBreakerRuleOld, func(tx *BaseTx) error {
		if err := c.cleanCircuitBreakerRelation(cbr); err != nil {
			return store.Error(err)
		}

		if err := releaseCircuitBreaker(tx, cbr); err != nil {
			log.Errorf("[Store][CircuitBreaker] release rule err: %s", err.Error())
			return store.Error(err)
		}

		if err := tx.Commit(); err != nil {
			log.Errorf("[Store][database] fail to %s commit tx, release rule(%+v) commit tx err: %s",
				labelReleaseCircuitBreakerRuleOld, cbr, err.Error())
			return err
		}
		return nil
	})
}

// releaseCircuitBreaker 发布熔断规则的内部函数
// @note 可能存在服务的规则，由旧的更新到新的场景
func releaseCircuitBreaker(tx *BaseTx, cbr *model.CircuitBreakerRelation) error {
	// 发布规则时，需要保证规则已经被标记
	str := `insert into circuitbreaker_rule_relation(service_id, rule_id, rule_version, flag, ctime, mtime)
		select '%s', '%s', '%s', 0, '%s', '%s' from service, circuitbreaker_rule 
		where service.id = $1 and service.flag = 0 
		and circuitbreaker_rule.id = $2 and circuitbreaker_rule.version = $3 
		and circuitbreaker_rule.flag = 0 
		on DUPLICATE key update 
		rule_id = $4, rule_version = $5, flag = 0, mtime = '%s'`
	str = fmt.Sprintf(str, cbr.ServiceID, cbr.RuleID, cbr.RuleVersion, GetCurrentTimeFormat(), GetCurrentTimeFormat(), GetCurrentTimeFormat())
	log.Infof("[Store][CircuitBreaker] exec release sql(%s)", str)
	stmt, err := tx.Prepare(str)
	if err != nil {
		return err
	}
	result, err := stmt.Exec(cbr.ServiceID, cbr.RuleID, cbr.RuleVersion, cbr.RuleID, cbr.RuleVersion)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] release exec sql(%s) err: %s", str, err.Error())
		return err
	}
	if err := checkDataBaseAffectedRows(result, 1, 2); err != nil {
		if store.Code(err) == store.AffectedRowsNotMatch {
			return store.NewStatusError(store.NotFoundTagConfigOrService, "not found tag config or service")
		}
		log.Errorf("[Store][CircuitBreaker] release rule affected rows err: %s", err.Error())
		return err
	}

	return nil
}

// UnbindCircuitBreaker 解绑熔断规则
func (c *circuitBreakerStore) UnbindCircuitBreaker(serviceID, ruleID, ruleVersion string) error {
	return c.master.processWithTransaction(labelUnbindCircuitBreakerRuleOld, func(tx *BaseTx) error {
		str := `update circuitbreaker_rule_relation set flag = 1, mtime = $1 where service_id = $2 
                and rule_id = $3 and rule_version = $4`
		stmt, err := tx.Prepare(str)
		if err != nil {
			return err
		}
		if _, err = stmt.Exec(GetCurrentTimeFormat(), serviceID, ruleID, ruleVersion); err != nil {
			log.Errorf("[Store][CircuitBreaker] delete relation(%s) err: %s", serviceID, err.Error())
			return err
		}

		if err := tx.Commit(); err != nil {
			log.Errorf("[Store][database] fail to %s commit tx, unbind rule(%s) commit tx err: %s",
				labelUnbindCircuitBreakerRuleOld, ruleID, err.Error())
			return err
		}

		return nil
	})
}

// DeleteTagCircuitBreaker 删除非master熔断规则
func (c *circuitBreakerStore) DeleteTagCircuitBreaker(id string, version string) error {
	return c.master.processWithTransaction(labelDeleteTagCircuitBreakerRuleOld, func(tx *BaseTx) error {
		// 需要保证规则无绑定服务
		str := `update circuitbreaker_rule set flag = 1, mtime = $1
			where id = $2 and version = $3 
			and id not in 
			(select DISTINCT(rule_id) from circuitbreaker_rule_relation 
				where rule_id = $4 and rule_version = $5 and flag = 0)`
		log.Infof("[Store][circuitBreaker] delete rule id(%s) version(%s), sql(%s)", id, version, str)
		stmt, err := tx.Prepare(str)
		if err != nil {
			return err
		}
		if _, err = stmt.Exec(GetCurrentTimeFormat(), id, version, id, version); err != nil {
			log.Errorf("[Store][CircuitBreaker] delete tag rule(%s, %s) exec err: %s", id, version, err.Error())
			return err
		}

		if err := tx.Commit(); err != nil {
			log.Errorf("[Store][database] fail to %s commit tx, delete tag rule(%s) commit tx err: %s",
				labelDeleteTagCircuitBreakerRuleOld, id, err.Error())
			return err
		}
		return nil
	})
}

// DeleteMasterCircuitBreaker 删除master熔断规则
func (c *circuitBreakerStore) DeleteMasterCircuitBreaker(id string) error {
	return c.master.processWithTransaction(labelDeleteCircuitBreakerRuleOld, func(tx *BaseTx) error {
		// 需要保证所有已标记的规则无绑定服务
		str := `update circuitbreaker_rule set flag = 1, mtime = $1
			where id = $2 and version = 'master'
			and id not in 
			(select DISTINCT(rule_id) from circuitbreaker_rule_relation 
				where rule_id = $3 and flag = 0)`
		log.Infof("[Store][CircuitBreaker] delete master rule(%s) sql(%s)", id, str)
		stmt, err := tx.Prepare(str)
		if err != nil {
			return err
		}
		if _, err = stmt.Exec(GetCurrentTimeFormat(), id, id); err != nil {
			log.Errorf("[Store][CircuitBreaker] delete master rule(%s) exec err: %s", id, err.Error())
			return err
		}

		if err := tx.Commit(); err != nil {
			log.Errorf("[Store][database] fail to %s commit tx, delete rule(%s) commit tx err: %s",
				labelDeleteCircuitBreakerRuleOld, id, err.Error())
			return err
		}
		return nil
	})
}

// UpdateCircuitBreaker 修改熔断规则
// @note 只允许修改master熔断规则
func (c *circuitBreakerStore) UpdateCircuitBreaker(cb *model.CircuitBreaker) error {
	return c.master.processWithTransaction(labelUpdateCircuitBreakerRuleOld, func(tx *BaseTx) error {
		str := `update circuitbreaker_rule set business = $1, department = $2, comment = $3,
			inbounds = $4, outbounds = $5, token = $6, owner = $7, revision = $8, mtime = $9 
			where id = $10 and version = $11`
		stmt, err := tx.Prepare(str)
		if err != nil {
			return err
		}
		if _, err = stmt.Exec(cb.Business, cb.Department, cb.Comment, cb.Inbounds,
			cb.Outbounds, cb.Token, cb.Owner, cb.Revision, GetCurrentTimeFormat(),
			cb.ID, cb.Version); err != nil {
			log.Errorf("[Store][CircuitBreaker] update rule(%s,%s) exec err: %s", cb.ID, cb.Version, err.Error())
			return err
		}

		if err := tx.Commit(); err != nil {
			log.Errorf("[Store][database] fail to %s commit tx, update rule(%+v) commit tx err: %s",
				labelUpdateCircuitBreakerRuleOld, cb, err.Error())
			return err
		}
		return nil
	})
}

// GetCircuitBreaker 获取熔断规则
func (c *circuitBreakerStore) GetCircuitBreaker(id, version string) (*model.CircuitBreaker, error) {
	str := `select id, version, name, namespace, business, department, COALESCE(comment, ""),
			inbounds, outbounds, token, owner, revision, flag, ctime, mtime 
			from circuitbreaker_rule 
			where id = $1 and version = $2 and flag = 0`
	rows, err := c.master.Query(str, id, version)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] query circuitbreaker_rule with id(%s) and version(%s) err: %s",
			id, version, err.Error())
		return nil, err
	}

	out, err := fetchCircuitBreakerRows(rows)
	if err != nil {
		return nil, err
	}

	if len(out) == 0 {
		return nil, nil
	}

	return out[0], nil
}

// GetCircuitBreakerRelation 获取已标记熔断规则的绑定关系
func (c *circuitBreakerStore) GetCircuitBreakerRelation(ruleID, ruleVersion string) (
	[]*model.CircuitBreakerRelation, error) {
	str := genQueryCircuitBreakerRelation()
	str += `where rule_id = $1 and rule_version = $2 and flag = 0`
	rows, err := c.master.Query(str, ruleID, ruleVersion)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] query circuitbreaker_rule_relation "+
			"with rule_id(%s) and rule_version(%s) err: %s",
			ruleID, ruleVersion, err.Error())
		return nil, err
	}

	out, err := fetchCircuitBreakerRelationRows(rows)
	if err != nil {
		return nil, err
	}

	return out, nil
}

// GetCircuitBreakerMasterRelation 获取熔断规则master版本的绑定关系
func (c *circuitBreakerStore) GetCircuitBreakerMasterRelation(ruleID string) (
	[]*model.CircuitBreakerRelation, error) {
	str := genQueryCircuitBreakerRelation()
	str += `where rule_id = $1 and flag = 0`
	rows, err := c.master.Query(str, ruleID)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] query circuitbreaker_rule_relation with rule_id(%s) err: %s",
			ruleID, err.Error())
		return nil, err
	}

	out, err := fetchCircuitBreakerRelationRows(rows)
	if err != nil {
		return nil, err
	}

	return out, nil
}

// GetCircuitBreakerForCache 根据修改时间拉取增量熔断规则
func (c *circuitBreakerStore) GetCircuitBreakerForCache(mtime time.Time, firstUpdate bool) (
	[]*model.ServiceWithCircuitBreaker, error) {
	str := genQueryCircuitBreakerWithServiceID()
	str += `where circuitbreaker_rule_relation.mtime > $1 and rule_id = id and rule_version = version
			and circuitbreaker_rule.flag = 0`
	if firstUpdate {
		str += ` and circuitbreaker_rule_relation.flag != 1`
	}
	rows, err := c.slave.Query(str, mtime)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] query circuitbreaker_rule_relation with mtime err: %s",
			err.Error())
		return nil, err
	}
	circuitBreakers, err := fetchCircuitBreakerAndServiceRows(rows)
	if err != nil {
		return nil, err
	}
	return circuitBreakers, nil
}

// GetCircuitBreakerVersions 获取熔断规则的所有版本
func (c *circuitBreakerStore) GetCircuitBreakerVersions(id string) ([]string, error) {
	str := `select version from circuitbreaker_rule where id = $1 and flag = 0 order by mtime desc`
	rows, err := c.master.Query(str, id)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] get circuit breaker(%s) versions query err: %s", id, err.Error())
		return nil, err
	}

	var versions []string
	var version string
	for rows.Next() {
		if err := rows.Scan(&version); err != nil {
			log.Errorf("[Store][CircuitBreaker] get circuit breaker(%s) versions scan err: %s", id, err.Error())
			return nil, err
		}

		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		log.Errorf("[Store][CircuitBreaker] get circuit breaker(%s) versions next err: %s", id, err.Error())
		return nil, err
	}

	return versions, nil
}

// ListMasterCircuitBreakers 获取master熔断规则
func (c *circuitBreakerStore) ListMasterCircuitBreakers(filters map[string]string, offset uint32, limit uint32) (
	*model.CircuitBreakerDetail, error) {
	// 获取master熔断规则
	selectStr := `select rule.id, rule.version, rule.name, rule.namespace, rule.business,
				rule.department, rule.comment, rule.inbounds, rule.outbounds, rule.owner, 
				rule.revision, rule.ctime, rule.mtime from circuitbreaker_rule as rule `
	countStr := `select count(*) from circuitbreaker_rule as rule `
	whereStr := "where rule.version = 'master' and rule.flag = 0 "
	orderStr := "order by rule.mtime desc "
	pageStr := "limit $%d offset $%d "

	var (
		args  []interface{}
		index = 1
	)

	filterStr, filterArgs, index1 := genRuleFilterSQL("rule", filters, index)
	pageStr = fmt.Sprintf(pageStr, index1, index1+1)

	if filterStr != "" {
		whereStr += "and " + filterStr
		args = append(args, filterArgs...)
	}

	out := &model.CircuitBreakerDetail{
		Total:               0,
		CircuitBreakerInfos: make([]*model.CircuitBreakerInfo, 0),
	}
	err := c.master.QueryRow(countStr+whereStr, args...).Scan(&out.Total)
	switch {
	case err == sql.ErrNoRows:
		out.Total = 0
		return out, nil
	case err != nil:
		log.Errorf("[Store][CircuitBreaker] list master circuitbreakers query count err: %s", err.Error())
		return nil, err
	default:
	}

	args = append(args, limit)
	args = append(args, offset)

	rows, err := c.master.Query(selectStr+whereStr+orderStr+pageStr, args...)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] list master circuitbreaker query err: %s", err.Error())
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var entry model.CircuitBreaker
		if err := rows.Scan(&entry.ID, &entry.Version, &entry.Name, &entry.Namespace, &entry.Business,
			&entry.Department, &entry.Comment, &entry.Inbounds, &entry.Outbounds, &entry.Owner,
			&entry.Revision, &entry.CreateTime, &entry.ModifyTime); err != nil {
			log.Errorf("[Store][CircuitBreaker] list master circuitbreakers rows scan err: %s", err.Error())
			return nil, err
		}

		cbEntry := &model.CircuitBreakerInfo{CircuitBreaker: &entry}
		out.CircuitBreakerInfos = append(out.CircuitBreakerInfos, cbEntry)
	}
	if err := rows.Err(); err != nil {
		log.Errorf("[Store][CircuitBreaker] list master circuitbreakers rows next err: %s", err.Error())
		return nil, err
	}

	return out, nil
}

// ListReleaseCircuitBreakers 获取已发布规则及服务
func (c *circuitBreakerStore) ListReleaseCircuitBreakers(filters map[string]string, offset, limit uint32) (
	*model.CircuitBreakerDetail, error) {
	selectStr := `select rule_id, rule_version, relation.ctime, relation.mtime,
				name, namespace, service.owner from circuitbreaker_rule_relation as relation, service `
	whereStr := `where relation.flag = 0 and relation.service_id = service.id `
	orderStr := "order by relation.mtime desc "
	pageStr := "limit $%d offset $%d"

	countStr := `select count(*) from circuitbreaker_rule_relation as relation where relation.flag = 0 `

	var (
		args  []interface{}
		index = 1
	)
	filterStr, filterArgs, index1 := genRuleFilterSQL("relation", filters, index)
	pageStr = fmt.Sprintf(pageStr, index1, index1+1)

	if filterStr != "" {
		countStr += "and " + filterStr
		whereStr += "and " + filterStr
		args = append(args, filterArgs...)
	}

	out := &model.CircuitBreakerDetail{
		Total:               0,
		CircuitBreakerInfos: make([]*model.CircuitBreakerInfo, 0),
	}

	err := c.master.QueryRow(countStr, args...).Scan(&out.Total)
	switch {
	case err == sql.ErrNoRows:
		out.Total = 0
		return out, nil
	case err != nil:
		log.Errorf("[Store][CircuitBreaker] list tag circuitbreakers query count err: %s", err.Error())
		return nil, err
	default:
	}

	args = append(args, limit)
	args = append(args, offset)

	rows, err := c.master.Query(selectStr+whereStr+orderStr+pageStr, args...)
	if err != nil {
		log.Errorf("[Store][CircuitBreaker] list tag circuitBreakers query err: %s", err.Error())
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var entry model.CircuitBreaker
		var service model.Service
		if err := rows.Scan(&entry.ID, &entry.Version, &service.CreateTime, &service.ModifyTime,
			&service.Name, &service.Namespace, &service.Owner); err != nil {
			log.Errorf("[Store][CircuitBreaker] list tag circuitBreakers scan err: %s", err.Error())
			return nil, err
		}

		info := &model.CircuitBreakerInfo{
			CircuitBreaker: &entry,
			Services: []*model.Service{
				&service,
			},
		}

		out.CircuitBreakerInfos = append(out.CircuitBreakerInfos, info)
	}

	return out, nil
}

// GetCircuitBreakersByService 根据服务获取熔断规则
func (c *circuitBreakerStore) GetCircuitBreakersByService(name string, namespace string) (
	*model.CircuitBreaker, error) {
	str := `select rule.id, rule.version, rule.name, rule.namespace, rule.business, rule.comment, 
       		rule.department, rule.inbounds, rule.outbounds, rule.owner, rule.revision, rule.ctime, rule.mtime 
			from circuitbreaker_rule as rule, circuitbreaker_rule_relation as relation, service 
			where service.id = relation.service_id 
			and relation.rule_id = rule.id and relation.rule_version = rule.version
			and relation.flag = 0 and service.flag = 0 and rule.flag = 0 
			and service.name = $1 and service.namespace = $2`
	var breaker model.CircuitBreaker
	err := c.master.QueryRow(str, name, namespace).Scan(&breaker.ID, &breaker.Version, &breaker.Name,
		&breaker.Namespace, &breaker.Business, &breaker.Comment, &breaker.Department, &breaker.Inbounds,
		&breaker.Outbounds, &breaker.Owner, &breaker.Revision, &breaker.CreateTime, &breaker.ModifyTime)
	switch {
	case err == sql.ErrNoRows:
		return nil, nil
	case err != nil:
		log.Errorf("[Store][CircuitBreaker] get tag circuitbreaker with service(%s, %s) err: %s",
			name, namespace, err.Error())
		return nil, err
	default:
		return &breaker, nil
	}
}

// cleanCircuitBreakerRelation 清理无效的熔断规则关系
func (c *circuitBreakerStore) cleanCircuitBreakerRelation(cbr *model.CircuitBreakerRelation) error {
	log.Infof("[Store][CircuitBreaker] clean relation for service(%s)", cbr.ServiceID)
	str := `delete from circuitbreaker_rule_relation where service_id = $1 and flag = 1`
	stmt, err := c.master.Prepare(str)
	if err != nil {
		return err
	}
	if _, err = stmt.Exec(cbr.ServiceID); err != nil {
		log.Errorf("[Store][CircuitBreaker] clean relation service(%s) err: %s",
			cbr.ServiceID, err.Error())
		return err
	}

	return nil
}

// cleanCircuitBreaker 彻底清理熔断规则
func cleanCircuitBreaker(tx *BaseTx, id string, version string) error {
	str := `delete from circuitbreaker_rule where id = $1 and version = $2 and flag = 1`
	stmt, err := tx.Prepare(str)
	if err != nil {
		return store.Error(err)
	}
	if _, err = stmt.Exec(id, version); err != nil {
		log.Errorf("[Store][database] clean circuit breaker(%s) err: %s", id, err.Error())
		return store.Error(err)
	}
	return nil
}

// fetchCircuitBreakerRows 读取circuitbreaker_rule的数据
func fetchCircuitBreakerRows(rows *sql.Rows) ([]*model.CircuitBreaker, error) {
	defer rows.Close()
	var out []*model.CircuitBreaker
	for rows.Next() {
		var entry model.CircuitBreaker
		var flag int
		err := rows.Scan(&entry.ID, &entry.Version, &entry.Name, &entry.Namespace, &entry.Business, &entry.Department,
			&entry.Comment, &entry.Inbounds, &entry.Outbounds, &entry.Token, &entry.Owner, &entry.Revision,
			&flag, &entry.CreateTime, &entry.ModifyTime)
		if err != nil {
			log.Errorf("[Store][CircuitBreaker] fetch circuitbreaker_rule scan err: %s", err.Error())
			return nil, err
		}

		entry.Valid = true
		if flag == 1 {
			entry.Valid = false
		}

		out = append(out, &entry)
	}
	if err := rows.Err(); err != nil {
		log.Errorf("[Store][CircuitBreaker] fetch circuitbreaker_rule next err: %s", err.Error())
		return nil, err
	}

	return out, nil
}

// fetchCircuitBreakerRelationRows 读取circuitbreaker_rule_relation的数据
func fetchCircuitBreakerRelationRows(rows *sql.Rows) ([]*model.CircuitBreakerRelation, error) {
	defer rows.Close()
	var out []*model.CircuitBreakerRelation
	for rows.Next() {
		var entry model.CircuitBreakerRelation
		var flag int
		err := rows.Scan(&entry.ServiceID, &entry.RuleID, &entry.RuleVersion, &flag, &entry.CreateTime, &entry.ModifyTime)
		if err != nil {
			log.Errorf("[Store][CircuitBreaker] fetch circuitbreaker_rule_relation scan err: %s", err.Error())
			return nil, err
		}

		entry.Valid = true
		if flag == 1 {
			entry.Valid = false
		}

		out = append(out, &entry)
	}

	if err := rows.Err(); err != nil {
		log.Errorf("[Store][CircuitBreaker] fetch circuitbreaker_rule_relation next err: %s", err.Error())
		return nil, err
	}

	return out, nil
}

// fetchCircuitBreakerAndServiceRows 读取circuitbreaker_rule和circuitbreaker_rule_relation的数据
func fetchCircuitBreakerAndServiceRows(rows *sql.Rows) ([]*model.ServiceWithCircuitBreaker, error) {
	defer rows.Close()
	var out []*model.ServiceWithCircuitBreaker
	for rows.Next() {
		var entry model.ServiceWithCircuitBreaker
		var rule model.CircuitBreaker
		var relationFlag, ruleFlag int
		err := rows.Scan(&entry.ServiceID, &rule.ID, &rule.Version, &relationFlag, &entry.CreateTime,
			&entry.ModifyTime, &rule.Name, &rule.Namespace, &rule.Business, &rule.Department,
			&rule.Comment, &rule.Inbounds, &rule.Outbounds, &rule.Token, &rule.Owner, &rule.Revision,
			&ruleFlag, &rule.CreateTime, &rule.ModifyTime)
		if err != nil {
			log.Errorf("[Store][CircuitBreaker] fetch circuitbreaker_rule and relation scan err: %s",
				err.Error())
			return nil, err
		}
		entry.Valid = true
		if relationFlag == 1 {
			entry.Valid = false
		}
		rule.Valid = true
		if ruleFlag == 1 {
			rule.Valid = false
		}
		entry.CircuitBreaker = &rule
		out = append(out, &entry)
	}
	if err := rows.Err(); err != nil {
		log.Errorf("[Store][CircuitBreaker] fetch circuitbreaker_rule and relation next err: %s", err.Error())
		return nil, err
	}

	return out, nil
}

// genQueryCircuitBreakerRelation 查询熔断规则绑定关系表的语句
func genQueryCircuitBreakerRelation() string {
	str := `select service_id, rule_id, rule_version, flag, ctime, mtime
			from circuitbreaker_rule_relation `
	return str
}

// genQueryCircuitBreakerWithServiceID 根据服务id查询熔断规则的查询语句
func genQueryCircuitBreakerWithServiceID() string {
	str := `select service_id, rule_id, rule_version, circuitbreaker_rule_relation.flag,
			circuitbreaker_rule_relation.ctime, circuitbreaker_rule_relation.mtime, 
			name, namespace, business, department, comment, inbounds, outbounds, 
			token, owner, revision, circuitbreaker_rule.flag, 
			circuitbreaker_rule.ctime, circuitbreaker_rule.mtime 
			from circuitbreaker_rule_relation, circuitbreaker_rule `
	return str
}
