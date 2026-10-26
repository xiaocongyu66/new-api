//go:build dev

package identity

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/transport/contract"

	"gorm.io/gorm"
)

// DevAutoLoginUser is the throw-away account the dev endpoint signs in as. A
// fixed name keeps repeated calls idempotent instead of filling the database
// with one account per page load.
const DevAutoLoginUser = "dev-auto-login"

// devAutoLoginEnabled gates the endpoint at runtime. The `dev` build tag alone
// is deliberately not enough: a developer who builds with the tag but forgets
// to export the variable must not expose a login that skips every credential
// check on a reachable port.
func devAutoLoginEnabled() bool {
	return common.GetEnvOrDefaultBool("DEV_AUTO_LOGIN", false)
}

// DevAutoLogin provisions (or reuses) a super-admin and mints a real session
// for it, so the browser lands logged-in without a human typing credentials.
// It reuses the same session, cookie and audit machinery as a password login;
// only the credential step is skipped.
//
// The route itself is mounted by the transport layer (see compose's
// register_dev_autologin.go) because mounting middleware here would import a
// package that already imports identity.
func DevAutoLogin(c contract.Context) {
	if !devAutoLoginEnabled() {
		c.JSON(http.StatusNotFound, common.H{"success": false, "message": "not found"})
		return
	}

	user, err := ensureDevAutoLoginUser()
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	// loginMethodFromContext resolves the method from the request path, so the
	// session row and audit log record "dev-auto-login" rather than a real
	// password login.
	SetupLogin(user, c)
}

// ensureDevAutoLoginUser returns the shared dev account, creating it on first
// use. An existing account is repaired rather than trusted: a previous session
// may have demoted, disabled or otherwise broken it, and the developer would
// otherwise be silently locked out again.
func ensureDevAutoLoginUser() (*User, error) {
	var user User
	err := dbx.DB.Where("username = ?", DevAutoLoginUser).First(&user).Error
	if err == nil {
		return repairDevAutoLoginUser(&user)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	fresh := User{
		Username:    DevAutoLoginUser,
		DisplayName: DevAutoLoginUser,
		Password:    common.GetRandomString(32),
		Role:        common.RoleRootUser,
		Status:      common.UserStatusEnabled,
	}
	if err := fresh.Insert(0); err != nil {
		return nil, err
	}
	fresh.FinishInsert(0)
	return &fresh, nil
}

func repairDevAutoLoginUser(user *User) (*User, error) {
	needsWrite := false
	if user.Role != common.RoleRootUser {
		user.Role = common.RoleRootUser
		needsWrite = true
	}
	if user.Status != common.UserStatusEnabled {
		user.Status = common.UserStatusEnabled
		needsWrite = true
	}
	if !needsWrite {
		return user, nil
	}
	if err := user.Update(false); err != nil {
		return nil, err
	}
	// Publish so cached readers see the repaired role, and clear the in-memory
	// auto-ban mark that would otherwise survive until the process restarts.
	if err := PublishUserAuthCache(user.Id); err != nil {
		return nil, err
	}
	runUserUnbanHook(user.Id)
	return user, nil
}
