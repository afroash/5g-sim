package ue

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPISpawnListStop(t *testing.T) {
	mgr := NewManager(DefaultConfig(), ProfileLocal)
	srv := httptest.NewServer(NewServer(mgr).Handler())
	defer srv.Close()

	body := `{"profile":"local","supi":"imsi-001010000000088"}`
	resp, err := http.Post(srv.URL+"/v1/instances", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var created InstanceRecord
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	getResp, err := http.Get(srv.URL + "/v1/instances")
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Instances []InstanceRecord `json:"instances"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	getResp.Body.Close()
	if len(listed.Instances) != 1 {
		t.Fatalf("want 1 instance, got %d", len(listed.Instances))
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/instances/"+created.ID, nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status %d", delResp.StatusCode)
	}
}
