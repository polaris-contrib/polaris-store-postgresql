package postgresql

import (
	"database/sql"
	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/store"
	"time"
)

var _ store.ConfigFileReleaseStore = (*configFileReleaseStore)(nil)

type configFileReleaseStore struct {
	db    *BaseDB
	slave *BaseDB
}

// CreateConfigFileRelease 新建配置文件发布
func (cfr *configFileReleaseStore) CreateConfigFileRelease(tx store.Tx,
	fileRelease *model.ConfigFileRelease) (*model.ConfigFileRelease, error) {
	s := "insert into config_file_release(name, namespace, `group`, file_name, content, comment, md5, version, " +
		" create_time, create_by, modify_time, modify_by) values" +
		"(?,?,?,?,?,?,?,?, sysdate(),?,sysdate(),?)"
	var err error
	if tx != nil {
		_, err = tx.GetDelegateTx().(*BaseTx).Exec(s, fileRelease.Name, fileRelease.Namespace, fileRelease.Group,
			fileRelease.FileName, fileRelease.Content, fileRelease.Comment, fileRelease.Md5, fileRelease.Version,
			fileRelease.CreateBy, fileRelease.ModifyBy)
	} else {
		_, err = cfr.db.Exec(s, fileRelease.Name, fileRelease.Namespace, fileRelease.Group, fileRelease.FileName,
			fileRelease.Content, fileRelease.Comment, fileRelease.Md5, fileRelease.Version, fileRelease.CreateBy,
			fileRelease.ModifyBy)
	}
	if err != nil {
		return nil, store.Error(err)
	}
	return cfr.GetConfigFileRelease(tx, fileRelease.Namespace, fileRelease.Group, fileRelease.FileName)
}

// UpdateConfigFileRelease 更新配置文件发布
func (cfr *configFileReleaseStore) UpdateConfigFileRelease(tx store.Tx,
	fileRelease *model.ConfigFileRelease) (*model.ConfigFileRelease, error) {
	s := "update config_file_release set name = ? , content = ?, comment = ?, md5 = ?, version = ?, flag = 0, " +
		" modify_time = sysdate(), modify_by = ? where namespace = ? and `group` = ? and file_name = ?"
	var err error
	if tx != nil {
		_, err = tx.GetDelegateTx().(*BaseTx).Exec(s, fileRelease.Name, fileRelease.Content, fileRelease.Comment,
			fileRelease.Md5, fileRelease.Version, fileRelease.ModifyBy, fileRelease.Namespace, fileRelease.Group,
			fileRelease.FileName)
	} else {
		_, err = cfr.db.Exec(s, fileRelease.Name, fileRelease.Content, fileRelease.Comment, fileRelease.Md5,
			fileRelease.Version, fileRelease.ModifyBy, fileRelease.Namespace, fileRelease.Group, fileRelease.FileName)
	}
	if err != nil {
		return nil, store.Error(err)
	}
	return cfr.GetConfigFileRelease(tx, fileRelease.Namespace, fileRelease.Group, fileRelease.FileName)
}

// GetConfigFileRelease 获取配置文件发布，只返回 flag=0 的记录
func (cfr *configFileReleaseStore) GetConfigFileRelease(tx store.Tx, namespace,
	group, fileName string) (*model.ConfigFileRelease, error) {
	return cfr.getConfigFileReleaseByFlag(tx, namespace, group, fileName, false)
}

func (cfr *configFileReleaseStore) GetConfigFileReleaseWithAllFlag(tx store.Tx, namespace,
	group, fileName string) (*model.ConfigFileRelease, error) {
	return cfr.getConfigFileReleaseByFlag(tx, namespace, group, fileName, true)
}

func (cfr *configFileReleaseStore) getConfigFileReleaseByFlag(tx store.Tx, namespace, group,
	fileName string, withAllFlag bool) (*model.ConfigFileRelease, error) {
	querySql := cfr.baseQuerySql() + "where namespace = ? and `group` = ? and file_name = ? and flag = 0"

	if withAllFlag {
		querySql = cfr.baseQuerySql() + "where namespace = ? and `group` = ? and file_name = ?"
	}

	var (
		rows *sql.Rows
		err  error
	)

	if tx != nil {
		rows, err = tx.GetDelegateTx().(*BaseTx).Query(querySql, namespace, group, fileName)
	} else {
		rows, err = cfr.db.Query(querySql, namespace, group, fileName)
	}
	if err != nil {
		return nil, err
	}
	fileRelease, err := cfr.transferRows(rows)
	if err != nil {
		return nil, err
	}
	if len(fileRelease) > 0 {
		return fileRelease[0], nil
	}
	return nil, nil
}

func (cfr *configFileReleaseStore) DeleteConfigFileRelease(tx store.Tx, namespace, group,
	fileName, deleteBy string) error {
	s := "update config_file_release set flag = 1, modify_time = sysdate(), modify_by = ?, version = version + 1, " +
		" md5='' where namespace = ? and `group` = ? and file_name = ?"
	var err error
	if tx != nil {
		_, err = tx.GetDelegateTx().(*BaseTx).Exec(s, deleteBy, namespace, group, fileName)
	} else {
		_, err = cfr.db.Exec(s, deleteBy, namespace, group, fileName)
	}
	if err != nil {
		return store.Error(err)
	}
	return nil
}

// FindConfigFileReleaseByModifyTimeAfter 获取最后更新时间大于某个时间点的发布，注意包含 flag = 1 的，为了能够获取被删除的 release
func (cfr *configFileReleaseStore) FindConfigFileReleaseByModifyTimeAfter(
	modifyTime time.Time) ([]*model.ConfigFileRelease, error) {
	s := cfr.baseQuerySql() + " where modify_time > FROM_UNIXTIME(?)"
	rows, err := cfr.slave.Query(s, timeToTimestamp(modifyTime))
	if err != nil {
		return nil, err
	}
	releases, err := cfr.transferRows(rows)
	if err != nil {
		return nil, err
	}

	return releases, nil
}

func (cfr *configFileReleaseStore) CountConfigFileReleaseEachGroup() (map[string]map[string]int64, error) {
	metricsSql := "SELECT namespace, `group`, count(file_name) FROM config_file_release " +
		" WHERE flag = 0 GROUP by namespace, `group`"
	rows, err := cfr.slave.Query(metricsSql)
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

func (cfr *configFileReleaseStore) baseQuerySql() string {
	return "select id, name, namespace, `group`, file_name, content, IFNULL(comment, ''), md5, version, " +
		" UNIX_TIMESTAMP(create_time), IFNULL(create_by, ''), UNIX_TIMESTAMP(modify_time), IFNULL(modify_by, ''), " +
		" flag from config_file_release "
}

func (cfr *configFileReleaseStore) transferRows(rows *sql.Rows) ([]*model.ConfigFileRelease, error) {
	if rows == nil {
		return nil, nil
	}
	defer rows.Close()

	var fileReleases []*model.ConfigFileRelease

	for rows.Next() {
		fileRelease := &model.ConfigFileRelease{}
		var ctime, mtime int64
		err := rows.Scan(&fileRelease.Id, &fileRelease.Name, &fileRelease.Namespace, &fileRelease.Group,
			&fileRelease.FileName, &fileRelease.Content,
			&fileRelease.Comment, &fileRelease.Md5, &fileRelease.Version, &ctime, &fileRelease.CreateBy,
			&mtime, &fileRelease.ModifyBy, &fileRelease.Flag)
		if err != nil {
			return nil, err
		}
		fileRelease.CreateTime = time.Unix(ctime, 0)
		fileRelease.ModifyTime = time.Unix(mtime, 0)

		fileReleases = append(fileReleases, fileRelease)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return fileReleases, nil
}
