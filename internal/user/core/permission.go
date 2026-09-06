package core

import (
	"context"
	"fmt"
)

// ResolvePermissionSet resolves a user's live permission set (AD-12): the
// additive union of their permission-group memberships and direct grants —
// no denies, never subtracts (Story 2.2). It wraps the persistence backbone
// (ListPermissionsByUser, union + DISTINCT) and is resolved LIVE per request:
// nothing is cached, so a permission or membership change takes effect on the
// very next request (AD-2/AD-6/FR-21/FR-22).
//
// It is the first-class capability other modules (Tool, Admin) consume through
// the User Service port to authorize an action; the auth gateway enforces the
// same live set per request and answers 403 when the required code is missing
// (AD-2/AD-6).
func (s *Service) ResolvePermissionSet(ctx context.Context, user *User) ([]string, error) {
	if user == nil || user.ID == "" {
		return nil, ErrInvalidCredentials
	}
	perms, err := s.repo.ListPermissionsByUser(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to resolve permissions: %w", err)
	}
	return perms, nil
}