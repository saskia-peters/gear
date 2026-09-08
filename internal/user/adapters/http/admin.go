package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
	"github.com/saskia-peters/gear/internal/user/ports"
)

// AdminRoutes returns the isolated admin-module router (Story 2.1, review
// finding 2.1-1): the admin-status root plus the admin-recovery surface (FR-27)
// mounted at /recovery — the whole admin module lives in ONE URL space
// (/api/v1/admin). The gateway itself is applied at the mount point in the
// composition root (RequireAdminPermission with an admin-only permission), so
// this router only carries admin surfaces. The 404/405 responders answer with
// the uniform JSON envelope so no admin sub-path can ever emit a plain-text
// body.
//
// The user-approval surface (Story 2.4, FR-20) lives under /users and is gated
// by its OWN `users.approve` permission (AD-6): the outer mount gates the whole
// group with an admin-only code, and this per-route gate makes the approval
// endpoints reachable ONLY by holders of `users.approve` (Design Notes spec
// 2.4). The gateway is re-applied here with the real auth middleware so the 403
// stays the uniform hidden-existence envelope (FR-19).
func (h *Handler) AdminRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/", h.adminStatus)
	r.Post("/recovery/request", h.AdminRecoveryRequest)
	r.Post("/recovery/approve", h.AdminRecoveryApprove)
	r.Post("/recovery/deny", h.AdminRecoveryDeny)
	r.Get("/recovery/pending", h.AdminRecoveryPending)

	// User surface (Story 2.4 + 2.6): the /users sub-mount now gates on ANY of
	// the three `users.*` codes (users.view/users.manage/users.approve — the
	// same codes the SPA nav uses for the Benutzer entry), so a caller without
	// any of them gets the uniform 403 with no admin hint (FR-19). The approval
	// endpoints (pending/approve/reject) STILL require `users.approve`: the core
	// re-verifies the exact code per action defense-in-depth (Design Notes spec
	// 2.6) — a users.view-only holder can list/detail but never approve/reject,
	// and create/edit/deactivate need `users.manage`.
	users := chi.NewRouter()
	users.NotFound(httpapi.NotFoundHandler())
	users.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	users.Use(auth.RequireAnyPermission(h.validator, userApprovalResolver{h.service},
		[]string{core.UserViewPermission, core.UserApprovePermission, core.UserManagePermission, core.UsersQualificationsManagePermission},
		"users access denied", h.logger))
	users.Get("/pending", h.ListPendingUsers)
	users.Post("/{userID}/approve", h.ApproveUser)
	users.Post("/{userID}/reject", h.RejectUser)
	users.Get("/", h.ListAdminUsers)
	users.Post("/", h.CreateAdminUser)
	users.Get("/{userID}", h.GetAdminUserDetail)
	users.Put("/{userID}", h.UpdateAdminUser)
	users.Post("/{userID}/deactivate", h.DeactivateAdminUser)
	// User↔user-group membership from the user detail (Effort 2): replace a
	// user's organisational group set, gated by `user_groups.manage`.
	users.Put("/{userID}/groups", h.AssignUserGroupsHandler)
	// Per-user qualification assignment (Spec 2.9): gated by
	// `users.qualifications.manage` — fuehrende/schirrmeister/admin assign,
	// revoke, and edit a user's per-qualification valid-until here.
	users.Post("/{userID}/qualifications/{qualificationID}", h.AssignUserQualificationHandler)
	users.Delete("/{userID}/qualifications/{qualificationID}", h.RevokeUserQualificationHandler)
	users.Put("/{userID}/qualifications/{qualificationID}/expiry", h.UpdateUserQualificationExpiryHandler)
	r.Mount("/users", users)

	// Organisational user-group surface (Story 2.6, AD-12): a dedicated
	// user-groups sub-mount gated by `user_groups.manage` (the same code the
	// base series defines), so a caller without it gets the uniform 403 with no
	// admin hint (FR-19). User groups are ORGANISATIONAL ONLY — membership
	// grants no permission; the resolution query never joins user_groups.
	userGroups := chi.NewRouter()
	userGroups.NotFound(httpapi.NotFoundHandler())
	userGroups.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	userGroups.Use(auth.RequireAdminPermission(h.validator, userApprovalResolver{h.service}, core.UserGroupsManagePermission, h.logger))
	userGroups.Get("/", h.ListAdminUserGroups)
	userGroups.Post("/", h.CreateAdminUserGroup)
	userGroups.Get("/{groupID}/members", h.ListAdminUserGroupMembers)
	userGroups.Post("/{groupID}/members", h.AssignAdminUserGroupMembers)
	userGroups.Get("/{groupID}/roles", h.ListUserGroupRolesHandler)
	userGroups.Post("/{groupID}/roles", h.AssignUserGroupRolesHandler)
	userGroups.Delete("/{groupID}", h.DeleteAdminUserGroup)
	r.Mount("/user-groups", userGroups)

	// Qualification Management surface (Story 2.7, AD-7/FR-22): a dedicated
	// qualifications sub-mount gated by `qualifications.manage` (the same code
	// the SPA nav uses for the Qualifikationen entry, Story 2.3), so a caller
	// without it gets the uniform 403 with no admin hint (FR-19). Assigning/
	// removing takes effect immediately on the next qualification check because
	// resolution is live per request (AD-7/FR-22) — nothing here caches.
	qualifications := chi.NewRouter()
	qualifications.NotFound(httpapi.NotFoundHandler())
	qualifications.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	qualifications.Use(auth.RequireAnyPermission(h.validator, userApprovalResolver{h.service},
		[]string{core.QualificationsManagePermission, core.UsersQualificationsManagePermission},
		"qualifications access denied", h.logger))
	qualifications.Get("/", h.ListAdminQualifications)
	qualifications.Post("/", h.CreateAdminQualification)
	qualifications.Put("/{id}", h.UpdateAdminQualification)
	qualifications.Get("/{id}/assignees", h.ListAdminQualificationAssignees)
	qualifications.Post("/{id}/assignees", h.AssignAdminQualificationUsers)
	r.Mount("/qualifications", qualifications)

	// Role & Permission-Group surface (Story 2.5): a dedicated groups sub-mount
	// gated by ANY of the three `roles.*` codes (roles.create/roles.edit/
	// roles.assign — the same codes the SPA nav uses for the Rollen entry), so
	// a caller without any of them gets the uniform 403 with no admin hint
	// (FR-19). The list is reachable by any holder; create is additionally
	// guarded by `roles.create` and update by `roles.edit` inside the handlers/
	// core (defense-in-depth), so an assign-only holder can list but never
	// create/edit (Design Notes spec 2.5).
	groups := chi.NewRouter()
	groups.NotFound(httpapi.NotFoundHandler())
	groups.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	groups.Use(auth.RequireAnyPermission(h.validator, userApprovalResolver{h.service},
		[]string{core.RoleCreatePermission, core.RoleEditPermission, core.RoleAssignPermission},
		"roles access denied", h.logger))
	groups.Get("/", h.ListRoles)
	groups.Post("/", h.CreateRole)
	groups.Put("/{groupID}", h.UpdateRole)
	r.Mount("/groups", groups)

	return r
}

// userApprovalResolver adapts the service's ResolvePermissionSet to the auth
// gateway's PermissionResolver interface so the users sub-mount can be gated
// by the REAL RequireAdminPermission middleware (the service port resolves the
// live set per request, AD-12). The resolver only needs the caller's ID, so a
// bare user value suffices.
type userApprovalResolver struct {
	svc ports.Service
}

func (r userApprovalResolver) ListPermissionsByUser(ctx context.Context, userID string) ([]string, error) {
	if r.svc == nil {
		return nil, errors.New("user http: service not wired for permission resolution")
	}
	return r.svc.ResolvePermissionSet(ctx, &core.User{ID: userID})
}

// adminStatus is the minimal admin root handler proving the /api/v1/admin
// gateway isolation (Story 2.1): a health-style admin-status payload, reachable
// only by an admin.
func (h *Handler) adminStatus(w http.ResponseWriter, _ *http.Request) {
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"module": "admin", "status": "ok"})
}