package kindefake

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// The fake serves three connection routes itself, from registerRawRoutes,
// instead of through the generated server:
//   - GET /api/v1/connections, because the live API lists plain connection
//     objects while the spec wraps each one in {"code", "message",
//     "connection"} (kinde-oss/kinde-go#53).
//   - POST /api/v1/connections and PATCH /api/v1/connections/{id}, because
//     the generated decoder rejects social-connection options with "unable
//     to detect sum type variant": no field is unique to the social variant
//     of the spec's options oneOf.

// connection is a connection held by the fake.
type connection struct {
	id          string
	name        string
	displayName string
	strategy    string
	// options are the options last sent, as decoded JSON. Kinde never
	// returns them.
	options map[string]any
}

// connectionItem is a connection in the shape the live API lists it.
type connectionItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Strategy    string `json:"strategy"`
}

func (c *connection) item() connectionItem {
	return connectionItem{ID: c.id, Name: c.name, DisplayName: c.displayName, Strategy: c.strategy}
}

// builtinConnections returns the sign-in methods every Kinde business starts
// with. Kinde lists them with the connections a business creates.
func builtinConnections() map[string]*connection {
	conns := map[string]*connection{}
	for _, c := range []connection{
		{id: "conn_email_password", name: "email-password", displayName: "Email + password", strategy: "email:password"},
		{id: "conn_email_otp", name: "email-otp", displayName: "Email + code", strategy: "email:otp"},
		{id: "conn_phone_otp", name: "phone-otp", displayName: "Phone + code", strategy: "phone:otp"},
		{id: "conn_username_password", name: "username-password", displayName: "Username + password", strategy: "username:password"},
		{id: "conn_username_otp", name: "username-otp", displayName: "Username + code", strategy: "username:otp"},
	} {
		conns[c.id] = &c
	}
	return conns
}

// serveListConnections lists connections in ID order, paginated with
// page_size (default 10) and starting_after.
func (f *Fake) serveListConnections(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pageSize := 10
	if s := q.Get("page_size"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_PAGE_SIZE", message: "page_size must be a positive integer"})
			return
		}
		pageSize = n
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	ids := slices.Sorted(maps.Keys(f.connections))
	start := 0
	if after := q.Get("starting_after"); after != "" {
		i, found := slices.BinarySearch(ids, after)
		if found {
			i++
		}
		start = i
	}
	end := min(start+pageSize, len(ids))
	items := make([]connectionItem, 0, end-start)
	for _, id := range ids[start:end] {
		items = append(items, f.connections[id].item())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":        "OK",
		"message":     "Success",
		"connections": items,
		"has_more":    end < len(ids),
	})
}

func (f *Fake) serveCreateConnection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string         `json:"name"`
		DisplayName string         `json:"display_name"`
		Strategy    string         `json:"strategy"`
		Options     map[string]any `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: err.Error()})
		return
	}
	if err := mgmt.CreateConnectionReqStrategy(body.Strategy).Validate(); err != nil {
		writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_STRATEGY", message: err.Error()})
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	c := &connection{
		id:          f.newID("conn"),
		name:        body.Name,
		displayName: body.DisplayName,
		strategy:    body.Strategy,
		options:     body.Options,
	}
	f.connections[c.id] = c
	writeJSON(w, http.StatusCreated, map[string]any{
		"message":    "Connection successfully created",
		"code":       "CONNECTION_CREATED",
		"connection": map[string]string{"id": c.id},
	})
}

func (f *Fake) serveUpdateConnection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        *string        `json:"name"`
		DisplayName *string        `json:"display_name"`
		Options     map[string]any `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: err.Error()})
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.connections[r.PathValue("connection_id")]
	if !ok {
		writeAPIError(w, &apiError{status: http.StatusNotFound, code: "CONNECTION_NOT_FOUND", message: "Connection not found"})
		return
	}
	if body.Name != nil {
		c.name = *body.Name
	}
	if body.DisplayName != nil {
		c.displayName = *body.DisplayName
	}
	if body.Options != nil {
		c.options = body.Options
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Connection successfully updated", "code": "CONNECTION_UPDATED"})
}

func (h handler) GetConnection(_ context.Context, params mgmt.GetConnectionParams) (mgmt.GetConnectionRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	c, ok := h.f.connections[params.ConnectionID]
	if !ok {
		return nil, notFound("CONNECTION_NOT_FOUND", "Connection not found")
	}
	return &mgmt.Connection{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("Success"),
		Connection: mgmt.NewOptConnectionConnection(mgmt.ConnectionConnection{
			ID:          mgmt.NewOptString(c.id),
			Name:        mgmt.NewOptString(c.name),
			DisplayName: mgmt.NewOptString(c.displayName),
			Strategy:    mgmt.NewOptString(c.strategy),
		}),
	}, nil
}

func (h handler) DeleteConnection(_ context.Context, params mgmt.DeleteConnectionParams) (mgmt.DeleteConnectionRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if _, ok := h.f.connections[params.ConnectionID]; !ok {
		return nil, notFound("CONNECTION_NOT_FOUND", "Connection not found")
	}
	delete(h.f.connections, params.ConnectionID)
	return &mgmt.SuccessResponse{
		Message: mgmt.NewOptString("Connection successfully deleted"),
		Code:    mgmt.NewOptString("CONNECTION_DELETED"),
	}, nil
}

// RemoveConnection deletes a connection behind the provider's back.
func (f *Fake) RemoveConnection(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.connections, id)
}

// ConnectionOptions returns a copy of the options last sent for a
// connection, or nil if none were sent.
func (f *Fake) ConnectionOptions(id string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.connections[id]; ok {
		return maps.Clone(c.options)
	}
	return nil
}
