package domains

import "testing"

func TestTenantHost(t *testing.T) {
	if got := TenantHost("doricontdemo", "tukifac.com"); got != "doricontdemo.tukifac.com" {
		t.Fatalf("got %q", got)
	}
	if got := TenantURL("doricontdemo", "tukifac.com"); got != "https://doricontdemo.tukifac.com" {
		t.Fatalf("got %q", got)
	}
}
