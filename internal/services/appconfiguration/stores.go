package appconfiguration

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const (
	storesBucket    = "appconfiguration.stores"
	dataPlaneBucket = "appconfiguration.dataplane.stores"
)

// ConfigurationStore represents an Azure App Configuration store resource.
type ConfigurationStore struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Type       string                  `json:"type"`
	Location   string                  `json:"location"`
	Tags       map[string]string       `json:"tags,omitempty"`
	Sku        Sku                     `json:"sku"`
	Properties ConfigurationProperties `json:"properties"`
}

// Sku defines the pricing tier of an App Configuration store (e.g. Standard, Free).
type Sku struct {
	Name string `json:"name"`
}

// ConfigurationProperties represents the detailed configuration properties.
type ConfigurationProperties struct {
	ProvisioningState         string `json:"provisioningState"`
	CreationDate              string `json:"creationDate"`
	Endpoint                  string `json:"endpoint"`
	DisableLocalAuth          bool   `json:"disableLocalAuth"`
	PublicNetworkAccess       string `json:"publicNetworkAccess,omitempty"`
	PurgeProtectionEnabled    bool   `json:"purgeProtectionEnabled"`
	SoftDeleteRetentionInDays int    `json:"softDeleteRetentionInDays"`
}

// ApiKey represents an API access key / connection string entry.
type ApiKey struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Value            string `json:"value"`
	ConnectionString string `json:"connectionString"`
	LastModified     string `json:"lastModified"`
	ReadOnly         bool   `json:"readOnly"`
}

type storeRequest struct {
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Sku        *Sku              `json:"sku,omitempty"`
	Properties struct {
		DisableLocalAuth          bool   `json:"disableLocalAuth"`
		PublicNetworkAccess       string `json:"publicNetworkAccess,omitempty"`
		PurgeProtectionEnabled    bool   `json:"purgeProtectionEnabled"`
		SoftDeleteRetentionInDays int    `json:"softDeleteRetentionInDays"`
	} `json:"properties"`
}

func storeKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + strings.ToLower(name)
}

func (s *Service) registerStores(mux *http.ServeMux) {
	const base = "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.AppConfiguration/configurationStores"

	mux.HandleFunc("GET "+base, s.listStores)
	mux.HandleFunc("PUT "+base+"/{configStoreName}", s.putStore)
	mux.HandleFunc("GET "+base+"/{configStoreName}", s.getStore)
	mux.HandleFunc("DELETE "+base+"/{configStoreName}", s.deleteStore)
	mux.HandleFunc("POST "+base+"/{configStoreName}/listKeys", s.listKeys)
}

func (s *Service) putStore(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("configStoreName")

	var req storeRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			server.WriteError(w, http.StatusBadRequest, "InvalidRequestBody", "malformed request JSON body")
			return
		}
	}

	if req.Location == "" {
		server.WriteError(w, http.StatusBadRequest, "MissingRequiredParameter", "the 'location' field is required")
		return
	}

	skuName := "Standard"
	if req.Sku != nil && req.Sku.Name != "" {
		skuName = req.Sku.Name
	}

	publicNetworkAccess := "Enabled"
	if req.Properties.PublicNetworkAccess != "" {
		publicNetworkAccess = req.Properties.PublicNetworkAccess
	}

	retentionDays := 7
	if req.Properties.SoftDeleteRetentionInDays > 0 {
		retentionDays = req.Properties.SoftDeleteRetentionInDays
	}

	endpoint := fmt.Sprintf("https://%s.azconfig.io", name)

	store := ConfigurationStore{
		ID:       fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.AppConfiguration/configurationStores/%s", subID, rg, name),
		Name:     name,
		Type:     "Microsoft.AppConfiguration/configurationStores",
		Location: req.Location,
		Tags:     req.Tags,
		Sku:      Sku{Name: skuName},
		Properties: ConfigurationProperties{
			ProvisioningState:         "Succeeded",
			CreationDate:              time.Now().UTC().Format(time.RFC3339),
			Endpoint:                  endpoint,
			DisableLocalAuth:          req.Properties.DisableLocalAuth,
			PublicNetworkAccess:       publicNetworkAccess,
			PurgeProtectionEnabled:    req.Properties.PurgeProtectionEnabled,
			SoftDeleteRetentionInDays: retentionDays,
		},
	}

	key := storeKey(subID, rg, name)
	if err := s.db.Put(storesBucket, key, store); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	// Update data-plane index for direct /{storeName}.appconfig/ routing
	_ = s.db.Put(dataPlaneBucket, strings.ToLower(name), key)

	server.WriteJSON(w, http.StatusOK, store)
}

func (s *Service) getStore(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("configStoreName")

	var store ConfigurationStore
	found, err := s.db.Get(storesBucket, storeKey(subID, rg, name), &store)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound", fmt.Sprintf("Configuration store %q was not found", name))
		return
	}

	server.WriteJSON(w, http.StatusOK, store)
}

func (s *Service) listStores(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	prefix := subID + "/" + rg + "/"

	var list []ConfigurationStore
	err := s.db.List(storesBucket, prefix, func(k string, raw []byte) error {
		var store ConfigurationStore
		if err := json.Unmarshal(raw, &store); err != nil {
			return err
		}
		list = append(list, store)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	if list == nil {
		list = []ConfigurationStore{}
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": list})
}

func (s *Service) deleteStore(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("configStoreName")

	key := storeKey(subID, rg, name)
	if err := s.db.Delete(storesBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	_ = s.db.Delete(dataPlaneBucket, strings.ToLower(name))

	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) listKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("configStoreName")

	var store ConfigurationStore
	found, err := s.db.Get(storesBucket, storeKey(subID, rg, name), &store)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound", fmt.Sprintf("Configuration store %q was not found", name))
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	endpoint := store.Properties.Endpoint

	genKey := func(salt string) (string, string) {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s/%s", subID, rg, name, salt)))
		keyID := hex.EncodeToString(h[:4])
		secret := base64.StdEncoding.EncodeToString(h[4:])
		return keyID, secret
	}

	pkID, pkSecret := genKey("primary")
	skID, skSecret := genKey("secondary")
	prkID, prkSecret := genKey("primary-ro")
	srkID, srkSecret := genKey("secondary-ro")

	keys := []ApiKey{
		{
			ID:               pkID,
			Name:             "Primary",
			Value:            pkSecret,
			ConnectionString: fmt.Sprintf("Endpoint=%s;Id=%s;Secret=%s", endpoint, pkID, pkSecret),
			LastModified:     now,
			ReadOnly:         false,
		},
		{
			ID:               skID,
			Name:             "Secondary",
			Value:            skSecret,
			ConnectionString: fmt.Sprintf("Endpoint=%s;Id=%s;Secret=%s", endpoint, skID, skSecret),
			LastModified:     now,
			ReadOnly:         false,
		},
		{
			ID:               prkID,
			Name:             "Primary Read Only",
			Value:            prkSecret,
			ConnectionString: fmt.Sprintf("Endpoint=%s;Id=%s;Secret=%s", endpoint, prkID, prkSecret),
			LastModified:     now,
			ReadOnly:         true,
		},
		{
			ID:               srkID,
			Name:             "Secondary Read Only",
			Value:            srkSecret,
			ConnectionString: fmt.Sprintf("Endpoint=%s;Id=%s;Secret=%s", endpoint, srkID, srkSecret),
			LastModified:     now,
			ReadOnly:         true,
		},
	}

	server.WriteJSON(w, http.StatusOK, map[string]any{"value": keys})
}
