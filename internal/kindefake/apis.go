package kindefake

import (
	"context"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// apiResource is an API registered in the fake.
type apiResource struct {
	id       string
	name     string
	audience string
}

func (h handler) AddAPIs(_ context.Context, req *mgmt.AddAPIsReq) (mgmt.AddAPIsRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a := &apiResource{id: h.f.newID("api"), name: req.Name, audience: req.Audience}
	h.f.apis[a.id] = a
	return &mgmt.CreateApisResponse{
		Message: mgmt.NewOptString("Success"),
		Code:    mgmt.NewOptString("OK"),
		API:     mgmt.NewOptCreateApisResponseAPI(mgmt.CreateApisResponseAPI{ID: mgmt.NewOptString(a.id)}),
	}, nil
}

func (h handler) GetAPI(_ context.Context, params mgmt.GetAPIParams) (mgmt.GetAPIRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, ok := h.f.apis[params.APIID]
	if !ok {
		return nil, notFound("API_NOT_FOUND", "API not found")
	}
	return &mgmt.GetAPIResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("success_response"),
		API: mgmt.NewOptGetAPIResponseAPI(mgmt.GetAPIResponseAPI{
			ID:              mgmt.NewOptString(a.id),
			Name:            mgmt.NewOptString(a.name),
			Audience:        mgmt.NewOptString(a.audience),
			IsManagementAPI: mgmt.NewOptBool(false),
		}),
	}, nil
}

func (h handler) DeleteAPI(_ context.Context, params mgmt.DeleteAPIParams) (mgmt.DeleteAPIRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if _, ok := h.f.apis[params.APIID]; !ok {
		return nil, notFound("API_NOT_FOUND", "API not found")
	}
	delete(h.f.apis, params.APIID)
	return &mgmt.DeleteAPIResponse{
		Message: mgmt.NewOptString("API successfully deleted"),
		Code:    mgmt.NewOptString("API_DELETED"),
	}, nil
}

// RemoveAPI deletes an API behind the provider's back.
func (f *Fake) RemoveAPI(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.apis, id)
}
