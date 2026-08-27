package model

import (
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"gorm.io/gorm"
)

// UserQuery scopes a query to live user rows, and TokenQuery to live token rows.
//
// Both records carry gorm.DeletedAt, so callers must not target the table by name:
// tx.Table("users") drops the soft-delete predicate GORM adds for Model(&User{}),
// which would silently let updates hit deleted rows. These accessors keep the
// scope attached to the record, so other domains can build quota and subscription
// updates without importing the record type.
func UserQuery(tx *gorm.DB) *gorm.DB {
	return tx.Model(&User{})
}

// TokenQuery scopes a query to live token rows.
func TokenQuery(tx *gorm.DB) *gorm.DB {
	return tx.Model(&Token{})
}

// UserQuotaRow is the projection other domains need when they read a user row to
// gate on balance: the id, the quota, and the soft-delete marker.
//
// DeletedAt is not decoration. GORM derives both the table and the soft-delete
// predicate from the destination type, so a projection that omits it silently
// starts matching deleted rows.
type UserQuotaRow struct {
	Id        int
	Quota     int
	Email     string
	DeletedAt gorm.DeletedAt
}

// TableName pins the projection to the users table, since the struct name would
// otherwise pluralise to user_quota_rows.
func (UserQuotaRow) TableName() string {
	return "users"
}

// LockUserRow reads one live user row for update, returning the projection above.
// Callers outside this package use it instead of naming the record type, so the
// row lock and the soft-delete scope stay owned here.
func LockUserRow(tx *gorm.DB, userID int) (UserQuotaRow, error) {
	var row UserQuotaRow
	err := dbx.LockForUpdate(tx).Where("id = ?", userID).First(&row).Error
	return row, err
}

// ReadUserQuota reads one live user's quota without locking.
func ReadUserQuota(tx *gorm.DB, userID int) (UserQuotaRow, error) {
	var row UserQuotaRow
	err := tx.Select("id", "quota").Where("id = ?", userID).First(&row).Error
	return row, err
}
