package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithARMCaseNormalization(t *testing.T) {
	tests := []struct {
		name     string
		inputURL string
		wantPath string
	}{
		{
			name:     "already canonical",
			inputURL: "/subscriptions/sub-123/resourceGroups/rg-my/providers/Microsoft.Network/virtualNetworks/vnet1",
			wantPath: "/subscriptions/sub-123/resourceGroups/rg-my/providers/Microsoft.Network/virtualNetworks/vnet1",
		},
		{
			name:     "all lowercase skeleton and provider",
			inputURL: "/subscriptions/sub-123/resourcegroups/rg-my/providers/microsoft.network/virtualnetworks/vnet-abc",
			wantPath: "/subscriptions/sub-123/resourceGroups/rg-my/providers/Microsoft.Network/virtualNetworks/vnet-abc",
		},
		{
			name:     "lowercase redis and cache provider",
			inputURL: "/subscriptions/sub-1/resourcegroups/rg-1/providers/microsoft.cache/redis/my-cache",
			wantPath: "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.Cache/redis/my-cache",
		},
		{
			name:     "lowercase postgres and flexibleservers",
			inputURL: "/subscriptions/sub-1/resourcegroups/rg-1/providers/microsoft.dbforpostgresql/flexibleservers/pg1",
			wantPath: "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.DBforPostgreSQL/flexibleServers/pg1",
		},
		{
			name:     "lowercase private endpoints",
			inputURL: "/subscriptions/sub-1/resourcegroups/rg-1/providers/microsoft.network/privateendpoints/pe1/privatednszonegroups/grp1",
			wantPath: "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.Network/privateEndpoints/pe1/privateDnsZoneGroups/grp1",
		},
		{
			name:     "lowercase file services and shares",
			inputURL: "/subscriptions/sub-1/resourcegroups/rg-1/providers/microsoft.storage/storageaccounts/acc1/fileservices/default/shares/share1",
			wantPath: "/subscriptions/sub-1/resourceGroups/rg-1/providers/Microsoft.Storage/storageAccounts/acc1/fileServices/default/shares/share1",
		},
		{
			name:     "collapse double and triple slashes",
			inputURL: "//subscriptions///sub-123//resourcegroups//rg-my///providers//microsoft.compute/virtualmachines//vm1",
			wantPath: "/subscriptions/sub-123/resourceGroups/rg-my/providers/Microsoft.Compute/virtualMachines/vm1",
		},
		{
			name:     "preserves custom casing in user resource names",
			inputURL: "/subscriptions/Sub-123/resourcegroups/MyCustomRG/providers/microsoft.keyvault/vaults/MyVaultName",
			wantPath: "/subscriptions/Sub-123/resourceGroups/MyCustomRG/providers/Microsoft.KeyVault/vaults/MyVaultName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var recordedPath string
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				recordedPath = r.URL.Path
				w.WriteHeader(http.StatusOK)
			})

			handler := withARMCaseNormalization(next)
			req := httptest.NewRequest("GET", tt.inputURL, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if recordedPath != tt.wantPath {
				t.Errorf("got %q, want %q", recordedPath, tt.wantPath)
			}
		})
	}
}

func TestCollapseSlashes(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"/", "/"},
		{"//", "/"},
		{"///", "/"},
		{"/a//b///c////d", "/a/b/c/d"},
		{"a//b", "a/b"},
	}

	for _, tc := range cases {
		got := collapseSlashes(tc.input)
		if got != tc.want {
			t.Errorf("collapseSlashes(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
