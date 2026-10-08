package kindefake

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// application is an application as the fake stores it.
type application struct {
	id           string
	name         string
	appType      string
	clientSecret string
	loginURI     mgmt.OptString
	homepageURI  mgmt.OptString
	logoutURIs   []string
	redirectURIs []string
	// connections holds the IDs of enabled connections in the order they
	// were enabled. The fake does not check that the connections exist.
	connections []string
}

// lookupApplication returns the application with the given ID or a 404.
// Callers must hold f.mu.
func (f *Fake) lookupApplication(id string) (*application, error) {
	a, ok := f.applications[id]
	if !ok {
		return nil, notFound("APPLICATION_NOT_FOUND", "Application not found")
	}
	return a, nil
}

// CreateApplication stores an application. As in Kinde, its client ID is
// its ID.
func (h handler) CreateApplication(_ context.Context, req *mgmt.CreateApplicationReq) (mgmt.CreateApplicationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	id := h.f.newID("app")
	a := &application{id: id, name: req.Name, appType: string(req.Type), clientSecret: id + "_secret"}
	h.f.applications[id] = a
	return &mgmt.CreateApplicationResponse{
		Code:    mgmt.NewOptString("APPLICATION_CREATED"),
		Message: mgmt.NewOptString("Application successfully created"),
		Application: mgmt.NewOptCreateApplicationResponseApplication(mgmt.CreateApplicationResponseApplication{
			ID:           mgmt.NewOptString(a.id),
			ClientID:     mgmt.NewOptString(a.id),
			ClientSecret: mgmt.NewOptString(a.clientSecret),
		}),
	}, nil
}

// GetApplication returns an application. Kinde serves its logout and
// redirect URIs from separate endpoints.
func (h handler) GetApplication(_ context.Context, params mgmt.GetApplicationParams) (mgmt.GetApplicationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, err := h.f.lookupApplication(params.ApplicationID)
	if err != nil {
		return nil, err
	}
	return &mgmt.GetApplicationResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("success_response"),
		Application: mgmt.NewOptGetApplicationResponseApplication(mgmt.GetApplicationResponseApplication{
			ID:           mgmt.NewOptString(a.id),
			Name:         mgmt.NewOptString(a.name),
			Type:         mgmt.NewOptGetApplicationResponseApplicationType(mgmt.GetApplicationResponseApplicationType(a.appType)),
			ClientID:     mgmt.NewOptString(a.id),
			ClientSecret: mgmt.NewOptString(a.clientSecret),
			LoginURI:     a.loginURI,
			HomepageURI:  a.homepageURI,
		}),
	}, nil
}

// UpdateApplication changes the fields the request sets and leaves the rest
// alone.
func (h handler) UpdateApplication(_ context.Context, req mgmt.OptUpdateApplicationReq, params mgmt.UpdateApplicationParams) (mgmt.UpdateApplicationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, err := h.f.lookupApplication(params.ApplicationID)
	if err != nil {
		return nil, err
	}
	u, _ := req.Get()
	if name, ok := u.Name.Get(); ok {
		a.name = name
	}
	if u.LoginURI.Set {
		a.loginURI = u.LoginURI
	}
	if u.HomepageURI.Set {
		a.homepageURI = u.HomepageURI
	}
	// A list that is sent replaces the stored one; an empty list clears it.
	if u.LogoutUris != nil {
		a.logoutURIs = slices.Clone(u.LogoutUris)
	}
	if u.RedirectUris != nil {
		a.redirectURIs = slices.Clone(u.RedirectUris)
	}
	return &mgmt.UpdateApplicationOK{}, nil
}

// DeleteApplication deletes an application.
func (h handler) DeleteApplication(_ context.Context, params mgmt.DeleteApplicationParams) (mgmt.DeleteApplicationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if _, err := h.f.lookupApplication(params.ApplicationID); err != nil {
		return nil, err
	}
	delete(h.f.applications, params.ApplicationID)
	return &mgmt.SuccessResponse{
		Code:    mgmt.NewOptString("APPLICATION_DELETED"),
		Message: mgmt.NewOptString("Application successfully deleted"),
	}, nil
}

// GetLogoutURLs returns an application's logout URIs.
func (h handler) GetLogoutURLs(_ context.Context, params mgmt.GetLogoutURLsParams) (mgmt.GetLogoutURLsRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, err := h.f.lookupApplication(params.AppID)
	if err != nil {
		return nil, err
	}
	return &mgmt.LogoutRedirectUrls{
		LogoutUrls: append([]string{}, a.logoutURIs...),
		Code:       mgmt.NewOptString("OK"),
		Message:    mgmt.NewOptString("Success"),
	}, nil
}

// GetCallbackURLs returns an application's redirect URIs.
func (h handler) GetCallbackURLs(_ context.Context, params mgmt.GetCallbackURLsParams) (mgmt.GetCallbackURLsRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, err := h.f.lookupApplication(params.AppID)
	if err != nil {
		return nil, err
	}
	return &mgmt.RedirectCallbackUrls{RedirectUrls: append([]string{}, a.redirectURIs...)}, nil
}

// EnableConnection records a connection as enabled for an application. It
// accepts any connection ID.
func (h handler) EnableConnection(_ context.Context, params mgmt.EnableConnectionParams) (mgmt.EnableConnectionRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, err := h.f.lookupApplication(params.ApplicationID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(a.connections, params.ConnectionID) {
		a.connections = append(a.connections, params.ConnectionID)
	}
	return &mgmt.EnableConnectionOK{}, nil
}

// RemoveConnection disables a connection for an application. It answers
// 404 if the connection is not enabled.
func (h handler) RemoveConnection(_ context.Context, params mgmt.RemoveConnectionParams) (mgmt.RemoveConnectionRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, err := h.f.lookupApplication(params.ApplicationID)
	if err != nil {
		return nil, err
	}
	i := slices.Index(a.connections, params.ConnectionID)
	if i < 0 {
		return nil, notFound("CONNECTION_NOT_FOUND", "Connection is not enabled for this application")
	}
	a.connections = slices.Delete(a.connections, i, i+1)
	return &mgmt.SuccessResponse{
		Code:    mgmt.NewOptString("CONNECTION_REMOVED"),
		Message: mgmt.NewOptString("Connection successfully removed"),
	}, nil
}

// serveApplicationConnections serves
// GET /api/v1/applications/{application_id}/connections in the shape the
// API really sends: plain connection objects, not the {"connection": {...}}
// envelope that the spec declares and the SDK decodes as empty. Items carry
// only the connection ID, because the fake records enabled IDs without
// looking the connections up. It pages with page_size (default 10) and
// starting_after, like GET /api/v1/connections.
func (f *Fake) serveApplicationConnections(w http.ResponseWriter, r *http.Request) {
	pageSize := 10
	if v := r.URL.Query().Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "kindefake: invalid page_size " + v})
			return
		}
		pageSize = n
	}

	f.mu.Lock()
	a, ok := f.applications[r.PathValue("application_id")]
	var ids []string
	if ok {
		ids = slices.Clone(a.connections)
	}
	f.mu.Unlock()
	if !ok {
		writeAPIError(w, &apiError{status: http.StatusNotFound, code: "APPLICATION_NOT_FOUND", message: "Application not found"})
		return
	}

	start := 0
	if after := r.URL.Query().Get("starting_after"); after != "" {
		i := slices.Index(ids, after)
		if i < 0 {
			writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "kindefake: unknown starting_after " + after})
			return
		}
		start = i + 1
	}
	end := min(start+pageSize, len(ids))

	type item struct {
		ID string `json:"id"`
	}
	items := make([]item, 0, end-start)
	for _, id := range ids[start:end] {
		items = append(items, item{ID: id})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":        "OK",
		"message":     "Success",
		"connections": items,
		"has_more":    end < len(ids),
	})
}

// RemoveApplication deletes an application behind the provider's back.
func (f *Fake) RemoveApplication(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.applications, id)
}

// SetApplicationURIs replaces an application's logout and redirect URIs
// behind the provider's back. It does nothing if the application does not
// exist.
func (f *Fake) SetApplicationURIs(id string, logoutURIs, redirectURIs []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.applications[id]; ok {
		a.logoutURIs = slices.Clone(logoutURIs)
		a.redirectURIs = slices.Clone(redirectURIs)
	}
}

// RemoveApplicationConnection disables a connection for an application
// behind the provider's back.
func (f *Fake) RemoveApplicationConnection(applicationID, connectionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.applications[applicationID]; ok {
		a.connections = slices.DeleteFunc(a.connections, func(id string) bool { return id == connectionID })
	}
}
