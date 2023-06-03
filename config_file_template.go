package postgresql

import (
	"database/sql"
	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/store"
	"time"
)

type configFileTemplateStore struct {
	db *BaseDB
}

// CreateConfigFileTemplate create config file template
func (cf *configFileTemplateStore) CreateConfigFileTemplate(
	template *model.ConfigFileTemplate) (*model.ConfigFileTemplate, error) {
	createSql := "insert into config_file_template(name,content,comment,format,create_time,create_by, " +
		" modify_time,modify_by) values " +
		"(?,?,?,?,sysdate(),?,sysdate(),?)"
	_, err := cf.db.Exec(createSql, template.Name, template.Content, template.Comment, template.Format,
		template.CreateBy, template.ModifyBy)
	if err != nil {
		return nil, store.Error(err)
	}

	return cf.GetConfigFileTemplate(template.Name)
}

// GetConfigFileTemplate get config file template by name
func (cf *configFileTemplateStore) GetConfigFileTemplate(name string) (*model.ConfigFileTemplate, error) {
	querySql := cf.baseSelectConfigFileTemplateSql() + " where name = ?"
	rows, err := cf.db.Query(querySql, name)
	if err != nil {
		return nil, store.Error(err)
	}

	templates, err := cf.transferRows(rows)
	if err != nil {
		return nil, err
	}
	if len(templates) > 0 {
		return templates[0], nil
	}
	return nil, nil
}

// QueryAllConfigFileTemplates query all config file templates
func (cf *configFileTemplateStore) QueryAllConfigFileTemplates() ([]*model.ConfigFileTemplate, error) {
	querySql := cf.baseSelectConfigFileTemplateSql() + " order by id desc"
	rows, err := cf.db.Query(querySql)
	if err != nil {
		return nil, store.Error(err)
	}

	templates, err := cf.transferRows(rows)
	if err != nil {
		return nil, err
	}
	return templates, nil
}

func (cf *configFileTemplateStore) baseSelectConfigFileTemplateSql() string {
	return "select id, name, content,IFNULL(comment, ''),format, UNIX_TIMESTAMP(create_time),  " +
		" IFNULL(create_by, ''),UNIX_TIMESTAMP(modify_time),IFNULL(modify_by, '') from config_file_template "
}

func (cf *configFileTemplateStore) transferRows(rows *sql.Rows) ([]*model.ConfigFileTemplate, error) {
	if rows == nil {
		return nil, nil
	}
	defer func() {
		_ = rows.Close()
	}()

	var templates []*model.ConfigFileTemplate
	for rows.Next() {
		template := &model.ConfigFileTemplate{}
		var ctime, mtime int64
		err := rows.Scan(&template.Id, &template.Name, &template.Content, &template.Comment, &template.Format,
			&ctime, &template.CreateBy, &mtime, &template.ModifyBy)
		if err != nil {
			return nil, err
		}
		template.CreateTime = time.Unix(ctime, 0)
		template.ModifyTime = time.Unix(mtime, 0)

		templates = append(templates, template)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return templates, nil
}
