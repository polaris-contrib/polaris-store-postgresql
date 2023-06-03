package postgresql

import (
	"database/sql"
	"fmt"
	"github.com/polarismesh/polaris/common/log"
	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/store"
	"strings"
	"time"
)

type configFileGroupStore struct {
	master *BaseDB
	slave  *BaseDB
}

// CreateConfigFileGroup 创建配置文件组
func (fg *configFileGroupStore) CreateConfigFileGroup(
	fileGroup *model.ConfigFileGroup) (*model.ConfigFileGroup, error) {
	createSql := "insert into config_file_group(name, namespace,comment,create_time, create_by, " +
		" modify_time, modify_by, owner)" +
		"value (?,?,?,sysdate(),?,sysdate(),?,?)"
	_, err := fg.master.Exec(createSql, fileGroup.Name, fileGroup.Namespace, fileGroup.Comment,
		fileGroup.CreateBy, fileGroup.ModifyBy, fileGroup.Owner)
	if err != nil {
		return nil, store.Error(err)
	}

	return fg.GetConfigFileGroup(fileGroup.Namespace, fileGroup.Name)
}

// GetConfigFileGroup 获取配置文件组
func (fg *configFileGroupStore) GetConfigFileGroup(namespace, name string) (*model.ConfigFileGroup, error) {
	querySql := fg.genConfigFileGroupSelectSql() + " where namespace=? and name=?"
	rows, err := fg.master.Query(querySql, namespace, name)
	if err != nil {
		return nil, store.Error(err)
	}
	cfgs, err := fg.transferRows(rows)
	if err != nil {
		return nil, err
	}
	if len(cfgs) > 0 {
		return cfgs[0], nil
	}
	return nil, nil
}

// QueryConfigFileGroups 翻页查询配置文件组, name 为模糊匹配关键字
func (fg *configFileGroupStore) QueryConfigFileGroups(namespace, name string,
	offset, limit uint32) (uint32, []*model.ConfigFileGroup, error) {
	name = "%" + name + "%"
	// 全部 namespace
	if namespace == "" {
		countSql := "select count(*) from config_file_group where name like ?"
		var count uint32
		err := fg.master.QueryRow(countSql, name).Scan(&count)
		if err != nil {
			return count, nil, err
		}

		s := fg.genConfigFileGroupSelectSql() + " where name like ? order by id desc limit ?,?"
		rows, err := fg.master.Query(s, name, offset, limit)
		if err != nil {
			return 0, nil, err
		}
		cfgs, err := fg.transferRows(rows)
		if err != nil {
			return 0, nil, err
		}

		return count, cfgs, nil
	}

	// 特定 namespace
	countSql := "select count(*) from config_file_group where namespace=? and name like ?"
	var count uint32
	err := fg.master.QueryRow(countSql, namespace, name).Scan(&count)
	if err != nil {
		return count, nil, err
	}

	s := fg.genConfigFileGroupSelectSql() + " where namespace=? and name like ? order by id desc limit ?,? "
	rows, err := fg.master.Query(s, namespace, name, offset, limit)
	if err != nil {
		return 0, nil, err
	}
	cfgs, err := fg.transferRows(rows)
	if err != nil {
		return 0, nil, err
	}

	return count, cfgs, nil
}

// DeleteConfigFileGroup 删除配置文件组
func (fg *configFileGroupStore) DeleteConfigFileGroup(namespace, name string) error {
	deleteSql := "delete from config_file_group where namespace = ? and name=?"

	log.Infof("[Config][Storage] delete config file group(%s, %s)", namespace, name)
	if _, err := fg.master.Exec(deleteSql, namespace, name); err != nil {
		return err
	}

	return nil
}

// UpdateConfigFileGroup 更新配置文件组信息
func (fg *configFileGroupStore) UpdateConfigFileGroup(
	fileGroup *model.ConfigFileGroup) (*model.ConfigFileGroup, error) {
	updateSql := "update config_file_group set comment = ?, modify_time = sysdate(), modify_by = ? " +
		" where namespace = ? and name = ?"
	_, err := fg.master.Exec(updateSql, fileGroup.Comment, fileGroup.ModifyBy, fileGroup.Namespace, fileGroup.Name)
	if err != nil {
		return nil, store.Error(err)
	}
	return fg.GetConfigFileGroup(fileGroup.Namespace, fileGroup.Name)
}

// FindConfigFileGroups 获取一组配置文件组信息
func (fg *configFileGroupStore) FindConfigFileGroups(namespace string,
	names []string) ([]*model.ConfigFileGroup, error) {
	querySql := fg.genConfigFileGroupSelectSql()
	params := make([]interface{}, 0)

	if namespace == "" {
		querySql += " where name in (%s)"
	} else {
		querySql += " where namespace = ? and name in (%s)"
		params = append(params, namespace)
	}

	inParamPlaceholders := make([]string, 0)
	for i := 0; i < len(names); i++ {
		inParamPlaceholders = append(inParamPlaceholders, "?")
		params = append(params, names[i])
	}
	querySql = fmt.Sprintf(querySql, strings.Join(inParamPlaceholders, ","))

	rows, err := fg.master.Query(querySql, params...)
	if err != nil {
		return nil, err
	}
	cfgs, err := fg.transferRows(rows)
	if err != nil {
		return nil, err
	}
	return cfgs, nil
}

func (fg *configFileGroupStore) GetConfigFileGroupById(id uint64) (*model.ConfigFileGroup, error) {
	querySql := fg.genConfigFileGroupSelectSql()
	querySql += fmt.Sprintf(" where id = %d", id)

	rows, err := fg.master.Query(querySql)
	if err != nil {
		return nil, err
	}

	cfgs, err := fg.transferRows(rows)
	if err != nil {
		return nil, err
	}
	if len(cfgs) == 0 {
		return nil, nil
	}

	return cfgs[0], nil
}

func (fg *configFileGroupStore) CountGroupEachNamespace() (map[string]int64, error) {
	metricsSql := "SELECT namespace, count(name) FROM config_file_group GROUP by namespace"
	rows, err := fg.slave.Query(metricsSql)
	if err != nil {
		return nil, store.Error(err)
	}

	defer func() {
		_ = rows.Close()
	}()

	ret := map[string]int64{}
	for rows.Next() {
		var (
			namespce string
			cnt      int64
		)

		if err := rows.Scan(&namespce, &cnt); err != nil {
			return nil, err
		}
		ret[namespce] = cnt
	}

	return ret, nil
}

func (fg *configFileGroupStore) genConfigFileGroupSelectSql() string {
	return "select id,name,namespace,IFNULL(comment,''),UNIX_TIMESTAMP(create_time),IFNULL(create_by,'')," +
		"UNIX_TIMESTAMP(modify_time),IFNULL(modify_by,''),IFNULL(owner,'') from config_file_group"
}

func (fg *configFileGroupStore) transferRows(rows *sql.Rows) ([]*model.ConfigFileGroup, error) {
	if rows == nil {
		return nil, nil
	}
	defer rows.Close()

	var fileGroups []*model.ConfigFileGroup

	for rows.Next() {
		fileGroup := &model.ConfigFileGroup{}
		var ctime, mtime int64
		err := rows.Scan(&fileGroup.Id, &fileGroup.Name, &fileGroup.Namespace, &fileGroup.Comment, &ctime,
			&fileGroup.CreateBy, &mtime, &fileGroup.ModifyBy, &fileGroup.Owner)
		if err != nil {
			return nil, err
		}
		fileGroup.CreateTime = time.Unix(ctime, 0)
		fileGroup.ModifyTime = time.Unix(mtime, 0)

		fileGroups = append(fileGroups, fileGroup)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return fileGroups, nil
}
