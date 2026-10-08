// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

type OrganizationUserResourceModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationCode types.String `tfsdk:"organization_code"`
	UserID           types.String `tfsdk:"user_id"`
	Roles            types.List   `tfsdk:"roles"`
	Permissions      types.List   `tfsdk:"permissions"`
}

// userNotInOrganization is the error code Kinde returns when a user is not a
// member of the organization. Kinde's OpenAPI spec does not document it.
const userNotInOrganization = "USER_NOT_IN_ORGANIZATION"

// membershipGone reports whether err means a membership no longer exists:
// the organization or user is gone, or the user has left the organization.
func membershipGone(err error) bool {
	return kindeapi.IsNotFound(err) || kindeapi.HasCode(err, userNotInOrganization)
}

// organizationUserRoleIDs returns the IDs of roles, in Kinde's order.
func organizationUserRoleIDs(roles []mgmt.OrganizationUserRole) []string {
	ids := make([]string, 0, len(roles))
	for _, role := range roles {
		if id, ok := role.ID.Get(); ok {
			ids = append(ids, id)
		}
	}
	return ids
}
