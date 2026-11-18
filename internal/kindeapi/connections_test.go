package kindeapi_test

import (
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// createConnection creates a Google connection with client credentials and
// returns its ID.
func createConnection(t *testing.T, c *kindeapi.Client, name string) string {
	t.Helper()
	created, err := c.CreateConnection(t.Context(), &mgmt.CreateConnectionReq{
		Name:        mgmt.NewOptString(name),
		DisplayName: mgmt.NewOptString("Display " + name),
		Strategy:    mgmt.NewOptCreateConnectionReqStrategy(mgmt.CreateConnectionReqStrategyOAuth2Google),
		Options: mgmt.NewOptCreateConnectionReqOptions(mgmt.NewCreateConnectionReqOptions0CreateConnectionReqOptions(
			mgmt.CreateConnectionReqOptions0{ClientID: mgmt.NewOptString("cid"), ClientSecret: mgmt.NewOptString("secret")},
		)),
	})
	if err != nil {
		t.Fatal(err)
	}
	id, ok := created.Connection.Value.ID.Get()
	if !ok || id == "" {
		t.Fatalf("CreateConnection returned no ID: %+v", created)
	}
	return id
}

func getConnection(t *testing.T, c *kindeapi.Client, id string) mgmt.ConnectionConnection {
	t.Helper()
	got, err := c.GetConnection(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	conn, ok := got.Connection.Get()
	if !ok {
		t.Fatalf("GetConnection returned no connection: %+v", got)
	}
	return conn
}

func TestConnectionRoundTrip(t *testing.T) {
	f, c := newFakeClient(t)
	id := createConnection(t, c, "google")

	conn := getConnection(t, c, id)
	if conn.ID.Value != id || conn.Name.Value != "google" || conn.DisplayName.Value != "Display google" || conn.Strategy.Value != "oauth2:google" {
		t.Fatalf("GetConnection = %+v, want the created connection", conn)
	}
	if got, want := f.ConnectionOptions(id), map[string]any{"client_id": "cid", "client_secret": "secret"}; !maps.Equal(got, want) {
		t.Fatalf("options sent = %v, want %v", got, want)
	}

	err := c.UpdateConnection(t.Context(), id, &mgmt.UpdateConnectionReq{
		DisplayName: mgmt.NewOptString("Google"),
		Options: mgmt.NewOptUpdateConnectionReqOptions(mgmt.NewUpdateConnectionReqOptions0UpdateConnectionReqOptions(
			mgmt.UpdateConnectionReqOptions0{ClientID: mgmt.NewOptString(""), ClientSecret: mgmt.NewOptString("")},
		)),
	})
	if err != nil {
		t.Fatal(err)
	}
	conn = getConnection(t, c, id)
	if conn.Name.Value != "google" || conn.DisplayName.Value != "Google" {
		t.Fatalf("after update: name %q, display name %q; want google, Google", conn.Name.Value, conn.DisplayName.Value)
	}
	if got, want := f.ConnectionOptions(id), map[string]any{"client_id": "", "client_secret": ""}; !maps.Equal(got, want) {
		t.Fatalf("options sent = %v, want %v", got, want)
	}

	if err := c.DeleteConnection(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetConnection(t.Context(), id); !kindeapi.IsNotFound(err) {
		t.Fatalf("GetConnection after delete: got %v, want not found", err)
	}
	if err := c.UpdateConnection(t.Context(), id, &mgmt.UpdateConnectionReq{Name: mgmt.NewOptString("x")}); !kindeapi.IsNotFound(err) {
		t.Fatalf("UpdateConnection after delete: got %v, want not found", err)
	}
	if err := c.DeleteConnection(t.Context(), id); !kindeapi.IsNotFound(err) {
		t.Fatalf("DeleteConnection after delete: got %v, want not found", err)
	}
}

func TestRemoveConnectionMakesGetConnectionNotFound(t *testing.T) {
	f, c := newFakeClient(t)
	id := createConnection(t, c, "google")
	f.RemoveConnection(id)
	if _, err := c.GetConnection(t.Context(), id); !kindeapi.IsNotFound(err) {
		t.Fatalf("got %v, want not found", err)
	}
}

func TestCreateConnectionRejectsUnknownStrategy(t *testing.T) {
	_, c := newFakeClient(t)
	_, err := c.CreateConnection(t.Context(), &mgmt.CreateConnectionReq{
		Name:     mgmt.NewOptString("bogus"),
		Strategy: mgmt.NewOptCreateConnectionReqStrategy("oauth2:bogus"),
	})
	if !kindeapi.HasCode(err, "INVALID_STRATEGY") {
		t.Fatalf("got %v, want INVALID_STRATEGY", err)
	}
}

func TestListConnectionsReturnsEveryPage(t *testing.T) {
	_, c := newFakeClient(t)
	var created []string
	for i := range 100 {
		created = append(created, createConnection(t, c, fmt.Sprintf("conn-%03d", i)))
	}

	conns, err := c.ListConnections(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// 100 created plus the 5 built-in connections: more than one page.
	if len(conns) != 105 {
		t.Fatalf("got %d connections, want 105", len(conns))
	}
	var ids []string
	for _, conn := range conns {
		if conn.Name == "" || conn.DisplayName == "" || conn.Strategy == "" {
			t.Fatalf("connection decoded with empty fields: %+v", conn)
		}
		ids = append(ids, conn.ID)
	}
	for _, id := range created {
		if !slices.Contains(ids, id) {
			t.Fatalf("connection %s missing from the list", id)
		}
	}
	if !slices.ContainsFunc(conns, func(conn kindeapi.Connection) bool { return conn.Strategy == "username:password" }) {
		t.Fatal("built-in username:password connection missing from the list")
	}
	slices.Sort(ids)
	if len(slices.Compact(ids)) != len(conns) {
		t.Fatal("the list repeats connections")
	}
}

// TestListConnectionsDecodesLiveShape pins the response shape the live API
// sends, taken from kinde-oss/kinde-go#63: plain connection objects, not the
// {"connection": {...}} envelope the spec describes.
func TestListConnectionsDecodesLiveShape(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"tok","token_type":"bearer","expires_in":3600}`)
	})
	mux.HandleFunc("GET /api/v1/connections", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("starting_after") == "" {
			_, _ = io.WriteString(w, `{"code":"OK","message":"Success","connections":[
				{"id":"conn_123","name":"saml","display_name":"SAML Connection","strategy":"saml"},
				{"id":"conn_456","name":"oauth","display_name":"OAuth Connection","strategy":"oauth"}
			],"has_more":true}`)
			return
		}
		_, _ = io.WriteString(w, `{"code":"OK","message":"Success","connections":[
			{"id":"conn_789","name":"google","strategy":"oauth2:google"}
		],"has_more":false}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := kindeapi.New(kindeapi.Config{Domain: srv.URL, Audience: srv.URL + "/api", ClientID: "id", ClientSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.ListConnections(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []kindeapi.Connection{
		{ID: "conn_123", Name: "saml", DisplayName: "SAML Connection", Strategy: "saml"},
		{ID: "conn_456", Name: "oauth", DisplayName: "OAuth Connection", Strategy: "oauth"},
		{ID: "conn_789", Name: "google", Strategy: "oauth2:google"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if wantQueries := []string{"page_size=100", "page_size=100&starting_after=conn_456"}; !slices.Equal(queries, wantQueries) {
		t.Fatalf("queries = %q, want %q", queries, wantQueries)
	}
}
