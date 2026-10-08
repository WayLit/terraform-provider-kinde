package kindefake

import (
	"context"
	"net/http"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// organization is the fake's record of one organization.
type organization struct {
	// details is what GetOrganization returns.
	details mgmt.GetOrganizationResponse
	// users maps each member's user ID to the IDs of the roles they hold in
	// the organization, in the order they were added.
	users map[string][]string
}

// colorSchemes maps each theme code to the color scheme Kinde reports with
// it. Its keys are the only theme codes the fake accepts.
var colorSchemes = map[mgmt.GetOrganizationResponseThemeCode]mgmt.GetOrganizationResponseColorScheme{
	mgmt.GetOrganizationResponseThemeCodeLight:          mgmt.GetOrganizationResponseColorSchemeLight,
	mgmt.GetOrganizationResponseThemeCodeDark:           mgmt.GetOrganizationResponseColorSchemeDark,
	mgmt.GetOrganizationResponseThemeCodeUserPreference: mgmt.GetOrganizationResponseColorSchemeLightDark,
}

// findOrganization returns the organization with the given code, or Kinde's
// not-found error. Callers must hold f.mu.
func (f *Fake) findOrganization(code string) (*organization, error) {
	org, ok := f.organizations[code]
	if !ok {
		return nil, notFound("ORGANIZATION_NOT_FOUND", "Organization not found")
	}
	return org, nil
}

// setTheme sets the theme code and the color scheme Kinde derives from it.
func setTheme(d *mgmt.GetOrganizationResponse, theme mgmt.GetOrganizationResponseThemeCode) error {
	scheme, ok := colorSchemes[theme]
	if !ok {
		return &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "theme_code must be light, dark, or user_preference"}
	}
	d.ThemeCode = mgmt.NewOptGetOrganizationResponseThemeCode(theme)
	d.ColorScheme = mgmt.NewOptGetOrganizationResponseColorScheme(scheme)
	return nil
}

// brandColor is how Kinde reports a brand color: raw, hex, and HSL forms.
// The fake fills the raw and hex forms with the value it was sent.
func brandColor(v string) mgmt.GetOrganizationResponseLinkColor {
	return mgmt.GetOrganizationResponseLinkColor{Raw: mgmt.NewOptString(v), Hex: mgmt.NewOptString(v)}
}

// setColors applies the brand colors a create or update request sets and
// leaves the others alone. Kinde's color types share one shape, so
// brandColor's result converts to each of them.
func setColors(d *mgmt.GetOrganizationResponse, background, button, buttonText, link mgmt.OptString) {
	if v, ok := background.Get(); ok {
		d.BackgroundColor = mgmt.NewOptNilGetOrganizationResponseBackgroundColor(mgmt.GetOrganizationResponseBackgroundColor(brandColor(v)))
	}
	if v, ok := button.Get(); ok {
		d.ButtonColor = mgmt.NewOptNilGetOrganizationResponseButtonColor(mgmt.GetOrganizationResponseButtonColor(brandColor(v)))
	}
	if v, ok := buttonText.Get(); ok {
		d.ButtonTextColor = mgmt.NewOptNilGetOrganizationResponseButtonTextColor(mgmt.GetOrganizationResponseButtonTextColor(brandColor(v)))
	}
	if v, ok := link.Get(); ok {
		d.LinkColor = mgmt.NewOptNilGetOrganizationResponseLinkColor(brandColor(v))
	}
}

// setNilString copies an optional request field into a nullable response
// field when the request sets it.
func setNilString(dst *mgmt.OptNilString, v mgmt.OptString) {
	if s, ok := v.Get(); ok {
		dst.SetTo(s)
	}
}

func (h handler) CreateOrganization(_ context.Context, req *mgmt.CreateOrganizationReq) (mgmt.CreateOrganizationRes, error) {
	if req.Name == "" {
		return nil, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "name is required"}
	}

	// Kinde reports unset optional fields as null.
	d := mgmt.GetOrganizationResponse{
		Name:      mgmt.NewOptString(req.Name),
		IsDefault: mgmt.NewOptBool(false),
		CreatedOn: mgmt.NewOptString(createdOn),
	}
	d.Handle.SetToNull()
	d.ExternalID.SetToNull()
	d.BackgroundColor.SetToNull()
	d.ButtonColor.SetToNull()
	d.ButtonTextColor.SetToNull()
	d.LinkColor.SetToNull()
	setNilString(&d.Handle, req.Handle)
	setNilString(&d.ExternalID, req.ExternalID)
	setColors(&d, req.BackgroundColor, req.ButtonColor, req.ButtonTextColor, req.LinkColor)
	theme := mgmt.GetOrganizationResponseThemeCodeLight
	if v, ok := req.ThemeCode.Get(); ok {
		theme = mgmt.GetOrganizationResponseThemeCode(v)
	}
	if err := setTheme(&d, theme); err != nil {
		return nil, err
	}

	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	code := h.f.newID("org")
	d.Code = mgmt.NewOptString(code)
	h.f.organizations[code] = &organization{details: d, users: map[string][]string{}}
	return &mgmt.CreateOrganizationResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("Organization successfully created"),
		Organization: mgmt.NewOptCreateOrganizationResponseOrganization(mgmt.CreateOrganizationResponseOrganization{
			Code: mgmt.NewOptString(code),
		}),
	}, nil
}

func (h handler) GetOrganization(_ context.Context, params mgmt.GetOrganizationParams) (mgmt.GetOrganizationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.findOrganization(params.Code)
	if err != nil {
		return nil, err
	}
	d := org.details
	return &d, nil
}

func (h handler) UpdateOrganization(_ context.Context, req mgmt.OptUpdateOrganizationReq, params mgmt.UpdateOrganizationParams) (mgmt.UpdateOrganizationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.findOrganization(params.OrgCode)
	if err != nil {
		return nil, err
	}

	// A request without a body changes nothing. Changes go to a copy so a
	// rejected request leaves the organization as it was.
	u := req.Or(mgmt.UpdateOrganizationReq{})
	d := org.details
	if v, ok := u.ThemeCode.Get(); ok {
		if err := setTheme(&d, mgmt.GetOrganizationResponseThemeCode(v)); err != nil {
			return nil, err
		}
	}
	if v, ok := u.Name.Get(); ok {
		d.Name = mgmt.NewOptString(v)
	}
	setNilString(&d.Handle, u.Handle)
	setNilString(&d.ExternalID, u.ExternalID)
	setColors(&d, u.BackgroundColor, u.ButtonColor, u.ButtonTextColor, u.LinkColor)
	org.details = d
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("Organization successfully updated")}, nil
}

func (h handler) DeleteOrganization(_ context.Context, params mgmt.DeleteOrganizationParams) (mgmt.DeleteOrganizationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if _, ok := h.f.organizations[params.OrgCode]; !ok {
		// This endpoint declares its 404 in the spec, with an object rather
		// than a list under "errors".
		return &mgmt.NotFoundResponse{Errors: mgmt.NewOptNotFoundResponseErrors(mgmt.NotFoundResponseErrors{
			Code:    mgmt.NewOptString("ORGANIZATION_NOT_FOUND"),
			Message: mgmt.NewOptString("Organization not found"),
		})}, nil
	}
	delete(h.f.organizations, params.OrgCode)
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("Organization successfully deleted")}, nil
}

// RemoveOrganization deletes an organization behind the provider's back.
func (f *Fake) RemoveOrganization(code string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.organizations, code)
}
