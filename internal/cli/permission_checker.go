package cli

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/permissionchecker"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// PermissionChecker provides pre-flight permission checking during dry-run.
type PermissionChecker = permissionchecker.PermissionChecker

// NewPermissionChecker creates a new PermissionChecker.
func NewPermissionChecker(client *openapigenerated.ClientWithResponses) *PermissionChecker {
	return permissionchecker.New(client)
}
