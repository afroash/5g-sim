package udm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestLoadSubscribers(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "subscribers.yaml")
	reg, err := LoadSubscribersFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.GetSubscriber("imsi-001010000000001"); !ok {
		t.Fatal("expected default subscriber")
	}
	if _, ok := reg.GetSubscriber("imsi-999999999999999"); ok {
		t.Fatal("unexpected subscriber")
	}
}

func TestUECMRegistrationHTTP(t *testing.T) {
	reg, err := LoadSubscribersFromFile(filepath.Join("..", "..", "configs", "subscribers.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	udm := New(DefaultConfig(), reg)
	ts := httptest.NewServer(http.HandlerFunc(udm.handleUECM))
	defer ts.Close()

	client := NewClient(ts.URL)
	data, err := client.RegisterAMF3GPPAccess("imsi-001010000000001", "amf-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.AllowedDnns) == 0 {
		t.Fatal("expected allowed DNNs")
	}

	_, err = client.RegisterAMF3GPPAccess("imsi-001010000000999", "amf-test")
	if err == nil {
		t.Fatal("expected error for unknown SUPI")
	}

	resp, err := http.Get(ts.URL + "/nudm-uecm/v1/imsi-001010000000002")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET subscription status %d", resp.StatusCode)
	}
	var sub SubscriptionData
	if err := json.NewDecoder(resp.Body).Decode(&sub); err != nil {
		t.Fatal(err)
	}
	if sub.SUPI == "" {
		t.Fatal("empty supi in response")
	}
}

func TestIsDnnAllowed(t *testing.T) {
	reg, err := LoadSubscribersFromFile(filepath.Join("..", "..", "configs", "subscribers.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reg.IsDnnAllowed("imsi-001010000000001", "internet") {
		t.Fatal("internet should be allowed")
	}
	if reg.IsDnnAllowed("imsi-001010000000001", "private") {
		t.Fatal("private should not be allowed")
	}
}
