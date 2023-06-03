package postgresql

import (
	"database/sql"
	"github.com/polarismesh/polaris/common/log"
	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/store"
	"time"
)

var _ store.ConfigFileStore = (*configFileStore)(nil)

type configFileStore struct {
	master *BaseDB
	slave  *BaseDB
}

// CreateConfigFile 创建配置文件
func (cf *configFileStore) CreateConfigFile(tx store.Tx, file *model.ConfigFile) (*model.ConfigFile, error) {
	err := cf.hardDeleteConfigFile(file.Namespace, file.Group, file.Name)
	if err != nil {
		return nil, err
	}
	createSql := "insert into config_file(name,namespace,`group`,content,comment,format,create_time, " +
		"create_by,modify_time,modify_by) values " +
		"(?,?,?,?,?,?,sysdate(),?,sysdate(),?)"
	if tx != nil {
		_, err = tx.GetDelegateTx().(*BaseTx).Exec(createSql, file.Name, file.Namespace, file.Group,
			file.Content, file.Comment, file.Format, file.CreateBy, file.ModifyBy)
	} else {
		_, err = cf.master.Exec(createSql, file.Name, file.Namespace, file.Group, file.Content, file.Comment,
			file.Format, file.CreateBy, file.ModifyBy)
	}
	if err != nil {
		return nil, store.Error(err)
	}
	return cf.GetConfigFile(tx, file.Namespace, file.Group, file.Name)
}

// GetConfigFile 获取配置文件
func (cf *configFileStore) GetConfigFile(tx store.Tx, namespace, group, name string) (*model.ConfigFile, error) {
	querySql := cf.baseSelectConfigFileSql() + "where namespace = ? and `group` = ? and name = ? and flag = 0"
	var rows *sql.Rows
	var err error
	if tx != nil {
		rows, err = tx.GetDelegateTx().(*BaseTx).Query(querySql, namespace, group, name)
	} else {
		rows, err = cf.master.Query(querySql, namespace, group, name)
	}
	if err != nil {
		return nil, err
	}
	files, err := cf.transferRows(rows)
	if err != nil {
		return nil, err
	}
	if len(files) > 0 {
		return files[0], nil
	}

	return nil, nil
}

func (cf *configFileStore) QueryConfigFilesByGroup(namespace, group string,
	offset, limit uint32) (uint32, []*model.ConfigFile, error) {
	var (
		countSql = "select count(*) from config_file where namespace = ? and `group` = ? and flag = 0"
		count    uint32
		err      = cf.master.QueryRow(countSql, namespace, group).Scan(&count)
	)

	if err != nil {
		return 0, nil, err
	}

	querySql := cf.baseSelectConfigFileSql() + "where namespace = ? and `group` = ? and flag = 0 order by id " +
		" desc limit ?,?"
	rows, err := cf.master.Query(querySql, namespace, group, offset, limit)
	if err != nil {
		return 0, nil, err
	}

	files, err := cf.transferRows(rows)
	if err != nil {
		return 0, nil, err
	}

	return count, files, nil
}

// QueryConfigFiles 翻页查询配置文件，group、name可为模糊匹配
func (cf *configFileStore) QueryConfigFiles(namespace, group, name string,
	offset, limit uint32) (uint32, []*model.ConfigFile, error) {
	// 全部 namespace
	if namespace == "" {
		group = "%" + group + "%"
		name = "%" + name + "%"
		countSql := "select count(*) from config_file where `group` like ? and name like ? and flag = 0"

		var count uint32
		err := cf.master.QueryRow(countSql, group, name).Scan(&count)
		if err != nil {
			return 0, nil, err
		}

		querySql := cf.baseSelectConfigFileSql() + "where `group` like ? and name like ? and flag = 0 " +
			" order by id desc limit ?,?"
		rows, err := cf.master.Query(querySql, group, name, offset, limit)
		if err != nil {
			return 0, nil, err
		}

		files, err := cf.transferRows(rows)
		if err != nil {
			return 0, nil, err
		}

		return count, files, nil
	}

	// 特定 namespace
	group = "%" + group + "%"
	name = "%" + name + "%"
	countSql := "select count(*) from config_file where namespace = ? and `group` like ? and name like ? and flag = 0"

	var count uint32
	err := cf.master.QueryRow(countSql, namespace, group, name).Scan(&count)
	if err != nil {
		return 0, nil, err
	}

	querySql := cf.baseSelectConfigFileSql() + "where namespace = ? and `group` like ? and name like ? " +
		" and flag = 0 order by id desc limit ?,?"
	rows, err := cf.master.Query(querySql, namespace, group, name, offset, limit)
	if err != nil {
		return 0, nil, err
	}

	files, err := cf.transferRows(rows)
	if err != nil {
		return 0, nil, err
	}

	return count, files, nil
}

// UpdateConfigFile 更新配置文件
func (cf *configFileStore) UpdateConfigFile(tx store.Tx, file *model.ConfigFile) (*model.ConfigFile, error) {
	updateSql := "update config_file set content = ? , comment = ?, format = ?, modify_time = sysdate(), " +
		" modify_by = ? where namespace = ? and `group` = ? and name = ?"
	var err error
	if tx != nil {
		_, err = tx.GetDelegateTx().(*BaseTx).Exec(updateSql, file.Content, file.Comment, file.Format,
			file.ModifyBy, file.Namespace, file.Group, file.Name)
	} else {
		_, err = cf.master.Exec(updateSql, file.Content, file.Comment, file.Format, file.ModifyBy,
			file.Namespace, file.Group, file.Name)
	}
	if err != nil {
		return nil, store.Error(err)
	}
	return cf.GetConfigFile(tx, file.Namespace, file.Group, file.Name)
}

// DeleteConfigFile 删除配置文件
func (cf *configFileStore) DeleteConfigFile(tx store.Tx, namespace, group, name string) error {
	deleteSql := "update config_file set flag = 1 where namespace = ? and `group` = ? and name = ?"
	var err error
	if tx != nil {
		_, err = tx.GetDelegateTx().(*BaseTx).Exec(deleteSql, namespace, group, name)
	} else {
		_, err = cf.master.Exec(deleteSql, namespace, group, name)
	}
	if err != nil {
		return store.Error(err)
	}
	return nil
}

func (cf *configFileStore) CountByConfigFileGroup(namespace, group string) (uint64, error) {
	countSql := "select count(*) from config_file where namespace = ? and `group` = ? and flag = 0"
	var count uint64
	err := cf.master.QueryRow(countSql, namespace, group).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (cf *configFileStore) CountConfigFileEachGroup() (map[string]map[string]int64, error) {
	metricsSql := "SELECT namespace, `group`, count(name) FROM config_file WHERE flag = 0 GROUP by namespace, `group`"
	rows, err := cf.slave.Query(metricsSql)
	if err != nil {
		return nil, store.Error(err)
	}

	defer func() {
		_ = rows.Close()
	}()

	ret := map[string]map[string]int64{}
	for rows.Next() {
		var (
			namespce string
			group    string
			cnt      int64
		)

		if err := rows.Scan(&namespce, &group, &cnt); err != nil {
			return nil, err
		}
		if _, ok := ret[namespce]; !ok {
			ret[namespce] = map[string]int64{}
		}
		ret[namespce][group] = cnt
	}

	return ret, nil
}

func (cf *configFileStore) baseSelectConfigFileSql() string {
	return "select id, name,namespace,`group`,content,IFNULL(comment, ''),format, UNIX_TIMESTAMP(create_time), " +
		" IFNULL(create_by, ''),UNIX_TIMESTAMP(modify_time),IFNULL(modify_by, '') from config_file "
}

func (cf *configFileStore) hardDeleteConfigFile(namespace, group, name string) error {
	log.Infof("[Config][Storage] delete config file. namespace = %s, group = %s, name = %s", namespace, group, name)

	deleteSql := "delete from config_file where namespace = ? and `group` = ? and name = ? and flag = 1"

	_, err := cf.master.Exec(deleteSql, namespace, group, name)
	if err != nil {
		return store.Error(err)
	}

	return nil
}

func (cf *configFileStore) transferRows(rows *sql.Rows) ([]*model.ConfigFile, error) {
	if rows == nil {
		return nil, nil
	}
	defer rows.Close()

	var files []*model.ConfigFile

	for rows.Next() {
		file := &model.ConfigFile{}
		var ctime, mtime int64
		err := rows.Scan(&file.Id, &file.Name, &file.Namespace, &file.Group, &file.Content, &file.Comment,
			&file.Format, &ctime, &file.CreateBy, &mtime, &file.ModifyBy)
		if err != nil {
			return nil, err
		}
		file.CreateTime = time.Unix(ctime, 0)
		file.ModifyTime = time.Unix(mtime, 0)

		files = append(files, file)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return files, nil
}
