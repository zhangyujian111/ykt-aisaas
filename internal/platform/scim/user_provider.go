// Package scim provides SCIM v2.0 user provider implementation.
package scim

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

// SCIMUserProvider defines the interface for SCIM user operations.
type SCIMUserProvider interface {
	GetByID(id string) (*SCIMUser, error)
	GetByUserName(username string) (*SCIMUser, error)
	GetByEmail(email string) (*SCIMUser, error)
	List(filter string) ([]*SCIMUser, error)
	Create(user *SCIMUser) error
	Update(userID string, user *SCIMUser) error
	Delete(userID string) error
	ApplyPatch(userID string, op SCIMPatchOp) error
}

// SCIMGroupProvider defines the interface for SCIM group operations.
type SCIMGroupProvider interface {
	GetByID(id string) (*SCIMGroup, error)
	GetByDisplayName(name string) (*SCIMGroup, error)
	List(filter string) ([]*SCIMGroup, error)
	Create(group *SCIMGroup) error
	Update(groupID string, group *SCIMGroup) error
	Delete(groupID string) error
}

// sysUserRow is the GORM model for sys_user table.
type sysUserRow struct {
	ID             int64          `gorm:"column:id;primaryKey" json:"id"`
	Username       string         `gorm:"column:username" json:"username"`
	Email          string         `gorm:"column:email" json:"email"`
	Active         bool           `gorm:"column:active" json:"active"`
	SCIMExternalID gorm.DeletedAt `gorm:"column:scim_external_id" json:"scim_external_id"`
	CreatedAt      time.Time      `gorm:"column:created_at" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at" json:"updated_at"`
}

func (sysUserRow) TableName() string {
	return "sys_user"
}

// MysqlSCIMUserProvider implements SCIMUserProvider using GORM.
type MysqlSCIMUserProvider struct {
	db *gorm.DB
}

// NewMysqlSCIMUserProvider creates a new GORM-backed SCIM user provider.
func NewMysqlSCIMUserProvider(db *gorm.DB) *MysqlSCIMUserProvider {
	return &MysqlSCIMUserProvider{db: db}
}

// GetByID retrieves a user by ID.
func (p *MysqlSCIMUserProvider) GetByID(id string) (*SCIMUser, error) {
	var row sysUserRow
	if err := p.db.Where("id = ? AND deleted_at IS NULL", id).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return p.rowToSCIMUser(&row)
}

// GetByUserName retrieves a user by userName.
func (p *MysqlSCIMUserProvider) GetByUserName(username string) (*SCIMUser, error) {
	var row sysUserRow
	if err := p.db.Where("username = ? AND deleted_at IS NULL", username).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return p.rowToSCIMUser(&row)
}

// GetByEmail retrieves a user by email.
func (p *MysqlSCIMUserProvider) GetByEmail(email string) (*SCIMUser, error) {
	var row sysUserRow
	if err := p.db.Where("email = ? AND deleted_at IS NULL", email).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return p.rowToSCIMUser(&row)
}

// List returns all users matching the filter.
func (p *MysqlSCIMUserProvider) List(filter string) ([]*SCIMUser, error) {
	var rows []sysUserRow
	query := p.db.Where("deleted_at IS NULL")

	if filter != "" {
		whereClause, whereArgs, err := (&FilterParser{}).Parse(filter)
		if err != nil {
			return nil, err
		}
		if whereClause != "" {
			query = query.Where(whereClause, whereArgs...)
		}
	}

	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}

	users := make([]*SCIMUser, 0, len(rows))
	for _, row := range rows {
		user, err := p.rowToSCIMUser(&row)
		if err != nil {
			continue
		}
		users = append(users, user)
	}

	return users, nil
}

// Create creates a new SCIM user.
func (p *MysqlSCIMUserProvider) Create(user *SCIMUser) error {
	email := ""
	if len(user.Emails) > 0 {
		email = user.Emails[0].Value
	}

	row := &sysUserRow{
		Username: user.UserName,
		Email:    email,
		Active:   user.Active,
	}

	if err := p.db.Create(row).Error; err != nil {
		return err
	}

	user.ID = fmt.Sprintf("%d", row.ID)
	return nil
}

// Update updates an existing SCIM user.
func (p *MysqlSCIMUserProvider) Update(userID string, user *SCIMUser) error {
	email := ""
	if len(user.Emails) > 0 {
		email = user.Emails[0].Value
	}

	return p.db.Model(&sysUserRow{}).
		Where("id = ? AND deleted_at IS NULL", userID).
		Updates(map[string]any{
			"username":    user.UserName,
			"email":       email,
			"active":      user.Active,
			"updated_at":  time.Now(),
		}).Error
}

// Delete soft-deletes a SCIM user.
func (p *MysqlSCIMUserProvider) Delete(userID string) error {
	return p.db.Model(&sysUserRow{}).
		Where("id = ?", userID).
		Update("deleted_at", time.Now()).Error
}

// ApplyPatch applies a SCIM PATCH operation.
func (p *MysqlSCIMUserProvider) ApplyPatch(userID string, op SCIMPatchOp) error {
	// Get current user
	user, err := p.GetByID(userID)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("user not found")
	}

	// Apply operations
	for _, operation := range op.Operations {
		switch operation.Op {
		case "add":
			p.applyAdd(user, operation.Path, operation.Value)
		case "replace":
			p.applyReplace(user, operation.Path, operation.Value)
		case "remove":
			p.applyRemove(user, operation.Path)
		}
	}

	// Update user
	return p.Update(userID, user)
}

func (p *MysqlSCIMUserProvider) applyAdd(user *SCIMUser, path string, value []byte) {
	if path == "emails" || path == "" {
		var emails []SCIMEmail
		if err := json.Unmarshal(value, &emails); err == nil {
			user.Emails = append(user.Emails, emails...)
		}
	} else if path == "active" {
		var active bool
		if err := json.Unmarshal(value, &active); err == nil {
			user.Active = active
		}
	}
}

func (p *MysqlSCIMUserProvider) applyReplace(user *SCIMUser, path string, value []byte) {
	if path == "emails" || path == "" {
		var emails []SCIMEmail
		if err := json.Unmarshal(value, &emails); err == nil {
			user.Emails = emails
		}
	} else if path == "active" {
		var active bool
		if err := json.Unmarshal(value, &active); err == nil {
			user.Active = active
		}
	} else if path == "userName" {
		var username string
		if err := json.Unmarshal(value, &username); err == nil {
			user.UserName = username
		}
	}
}

func (p *MysqlSCIMUserProvider) applyRemove(user *SCIMUser, path string) {
	if path == "emails" {
		user.Emails = []SCIMEmail{}
	} else if path == "active" {
		user.Active = false
	}
}

// rowToSCIMUser converts a database row to a SCIMUser.
func (p *MysqlSCIMUserProvider) rowToSCIMUser(row *sysUserRow) (*SCIMUser, error) {
	user := &SCIMUser{
		Schemas:  []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		ID:       fmt.Sprintf("%d", row.ID),
		UserName: row.Username,
		Active:   row.Active,
		Emails: []SCIMEmail{
			{Value: row.Email, Primary: true},
		},
		Meta: SCIMMeta{
			ResourceType: "User",
			Created:      row.CreatedAt.Format(time.RFC3339),
			Modified:     row.UpdatedAt.Format(time.RFC3339),
			Location:     fmt.Sprintf("/scim/v2/Users/%d", row.ID),
		},
	}

	return user, nil
}

// MysqlSCIMGroupProvider implements SCIMGroupProvider using GORM.
type MysqlSCIMGroupProvider struct {
	db *gorm.DB
}

// NewMysqlSCIMGroupProvider creates a new GORM-backed SCIM group provider.
func NewMysqlSCIMGroupProvider(db *gorm.DB) *MysqlSCIMGroupProvider {
	return &MysqlSCIMGroupProvider{db: db}
}

// sysGroupRow is the GORM model for sys_group table.
type sysGroupRow struct {
	ID          int64          `gorm:"column:id;primaryKey" json:"id"`
	DisplayName string         `gorm:"column:display_name" json:"display_name"`
	ExternalID  string         `gorm:"column:external_id" json:"external_id"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at" json:"deleted_at"`
	CreatedAt   time.Time      `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at" json:"updated_at"`
}

func (sysGroupRow) TableName() string {
	return "sys_group"
}

// GetByID retrieves a group by ID.
func (p *MysqlSCIMGroupProvider) GetByID(id string) (*SCIMGroup, error) {
	var row sysGroupRow
	if err := p.db.Where("id = ? AND deleted_at IS NULL", id).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return p.rowToSCIMGroup(&row)
}

// GetByDisplayName retrieves a group by displayName.
func (p *MysqlSCIMGroupProvider) GetByDisplayName(name string) (*SCIMGroup, error) {
	var row sysGroupRow
	if err := p.db.Where("display_name = ? AND deleted_at IS NULL", name).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return p.rowToSCIMGroup(&row)
}

// List returns all groups matching the filter.
func (p *MysqlSCIMGroupProvider) List(filter string) ([]*SCIMGroup, error) {
	var rows []sysGroupRow
	query := p.db.Where("deleted_at IS NULL")

	if filter != "" {
		whereClause, whereArgs, err := (&FilterParser{}).Parse(filter)
		if err != nil {
			return nil, err
		}
		if whereClause != "" {
			query = query.Where(whereClause, whereArgs...)
		}
	}

	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}

	groups := make([]*SCIMGroup, 0, len(rows))
	for _, row := range rows {
		group, err := p.rowToSCIMGroup(&row)
		if err != nil {
			continue
		}
		groups = append(groups, group)
	}

	return groups, nil
}

// Create creates a new SCIM group.
func (p *MysqlSCIMGroupProvider) Create(group *SCIMGroup) error {
	row := &sysGroupRow{
		DisplayName: group.DisplayName,
		ExternalID:  group.ExternalID,
	}

	if err := p.db.Create(row).Error; err != nil {
		return err
	}

	group.ID = fmt.Sprintf("%d", row.ID)
	return nil
}

// Update updates an existing SCIM group.
func (p *MysqlSCIMGroupProvider) Update(groupID string, group *SCIMGroup) error {
	return p.db.Model(&sysGroupRow{}).
		Where("id = ? AND deleted_at IS NULL", groupID).
		Updates(map[string]any{
			"display_name": group.DisplayName,
			"external_id":  group.ExternalID,
			"updated_at":   time.Now(),
		}).Error
}

// Delete soft-deletes a SCIM group.
func (p *MysqlSCIMGroupProvider) Delete(groupID string) error {
	return p.db.Model(&sysGroupRow{}).
		Where("id = ?", groupID).
		Update("deleted_at", time.Now()).Error
}

// rowToSCIMGroup converts a database row to a SCIMGroup.
func (p *MysqlSCIMGroupProvider) rowToSCIMGroup(row *sysGroupRow) (*SCIMGroup, error) {
	group := &SCIMGroup{
		Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
		ID:          fmt.Sprintf("%d", row.ID),
		DisplayName: row.DisplayName,
		ExternalID:  row.ExternalID,
		Meta: SCIMMeta{
			ResourceType: "Group",
			Created:      row.CreatedAt.Format(time.RFC3339),
			Modified:     row.UpdatedAt.Format(time.RFC3339),
			Location:     fmt.Sprintf("/scim/v2/Groups/%d", row.ID),
		},
	}

	return group, nil
}

// FilterParser parses SCIM filter expressions.
type FilterParser struct{}

// Parse parses a SCIM filter string into GORM WHERE clause and arguments.
// Supports: eq (equals), sw (startsWith), co (contains), gt (greaterThan), lt (lessThan).
func (p *FilterParser) Parse(filter string) (string, []interface{}, error) {
	if filter == "" {
		return "", nil, nil
	}

	// Pattern: attr op "value"
	// Examples: userName eq "alice", emails co "example"
	re := regexp.MustCompile(`(\w+)\s+(eq|sw|co|gt|lt)\s+"([^"]+)"`)
	matches := re.FindStringSubmatch(filter)
	if len(matches) != 4 {
		return "", nil, fmt.Errorf("unsupported filter syntax: %s", filter)
	}

	attr := matches[1]
	op := matches[2]
	value := matches[3]

	// Convert attribute name to column name
	column := camelToSnake(attr)

	// Build GORM expression
	switch op {
	case "eq":
		return fmt.Sprintf("%s = ?", column), []interface{}{value}, nil
	case "sw":
		return fmt.Sprintf("%s LIKE ?", column), []interface{}{value + "%"}, nil
	case "co":
		return fmt.Sprintf("%s LIKE ?", column), []interface{}{"%" + value + "%"}, nil
	case "gt":
		return fmt.Sprintf("%s > ?", column), []interface{}{value}, nil
	case "lt":
		return fmt.Sprintf("%s < ?", column), []interface{}{value}, nil
	}

	return "", nil, fmt.Errorf("unsupported operator: %s", op)
}

// camelToSnake converts camelCase to snake_case.
func camelToSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result.WriteByte('_')
			}
			result.WriteRune(r + 32) // lowercase
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}
