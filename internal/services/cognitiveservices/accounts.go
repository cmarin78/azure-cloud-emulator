package cognitiveservices

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const accountsBucket = "cognitiveservices.accounts"

// CognitiveAccount represents a Microsoft.CognitiveServices/accounts resource.
type CognitiveAccount struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Kind       string            `json:"kind"`
	Sku        CognitiveSku      `json:"sku"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties AccountProperties `json:"properties"`
}

// CognitiveSku defines the SKU tier of an account.
type CognitiveSku struct {
	Name string `json:"name"`
	Tier string `json:"tier,omitempty"`
}

// AccountProperties contains configuration and endpoint details.
type AccountProperties struct {
	Endpoint            string `json:"endpoint"`
	ProvisioningState   string `json:"provisioningState"`
	CustomSubDomainName string `json:"customSubDomainName,omitempty"`
	PublicNetworkAccess string `json:"publicNetworkAccess,omitempty"`
	DisableLocalAuth    bool   `json:"disableLocalAuth"`
}

// AccountKeys models the response of listKeys.
type AccountKeys struct {
	Key1 string `json:"key1"`
	Key2 string `json:"key2"`
}

type accountRequest struct {
	Location   string            `json:"location"`
	Kind       string            `json:"kind,omitempty"`
	Sku        *CognitiveSku     `json:"sku,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		CustomSubDomainName string `json:"customSubDomainName,omitempty"`
		PublicNetworkAccess string `json:"publicNetworkAccess,omitempty"`
		DisableLocalAuth    bool   `json:"disableLocalAuth"`
	} `json:"properties"`
}

func accountKey(subID, rg, name string) string {
	return subID + "/" + rg + "/" + strings.ToLower(name)
}

func (s *Service) registerAccounts(mux *http.ServeMux) {
	const base = "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.CognitiveServices/accounts"

	mux.HandleFunc("GET "+base, s.listAccounts)
	mux.HandleFunc("PUT "+base+"/{accountName}", s.putAccount)
	mux.HandleFunc("GET "+base+"/{accountName}", s.getAccount)
	mux.HandleFunc("DELETE "+base+"/{accountName}", s.deleteAccount)
	mux.HandleFunc("POST "+base+"/{accountName}/listKeys", s.listKeys)
}

func (s *Service) putAccount(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("accountName")

	var req accountRequest
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

	kind := "OpenAI"
	if req.Kind != "" {
		kind = req.Kind
	}

	skuName := "S0"
	if req.Sku != nil && req.Sku.Name != "" {
		skuName = req.Sku.Name
	}

	publicNetworkAccess := "Enabled"
	if req.Properties.PublicNetworkAccess != "" {
		publicNetworkAccess = req.Properties.PublicNetworkAccess
	}

	subDomain := name
	if req.Properties.CustomSubDomainName != "" {
		subDomain = req.Properties.CustomSubDomainName
	}

	endpoint := fmt.Sprintf("https://%s.cognitiveservices.azure.com/", subDomain)
	if strings.EqualFold(kind, "OpenAI") {
		endpoint = fmt.Sprintf("https://%s.openai.azure.com/", subDomain)
	}

	account := CognitiveAccount{
		ID:       fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.CognitiveServices/accounts/%s", subID, rg, name),
		Name:     name,
		Type:     "Microsoft.CognitiveServices/accounts",
		Location: req.Location,
		Kind:     kind,
		Sku:      CognitiveSku{Name: skuName, Tier: "Standard"},
		Tags:     req.Tags,
		Properties: AccountProperties{
			Endpoint:            endpoint,
			ProvisioningState:   "Succeeded",
			CustomSubDomainName: subDomain,
			PublicNetworkAccess: publicNetworkAccess,
			DisableLocalAuth:    req.Properties.DisableLocalAuth,
		},
	}

	key := accountKey(subID, rg, name)
	if err := s.db.Put(accountsBucket, key, account); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	server.WriteJSON(w, http.StatusOK, account)
}

func (s *Service) getAccount(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("accountName")

	var account CognitiveAccount
	found, err := s.db.Get(accountsBucket, accountKey(subID, rg, name), &account)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound", fmt.Sprintf("Account %q was not found", name))
		return
	}

	server.WriteJSON(w, http.StatusOK, account)
}

func (s *Service) listAccounts(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	prefix := subID + "/" + rg + "/"

	var list []CognitiveAccount
	err := s.db.List(accountsBucket, prefix, func(k string, raw []byte) error {
		var account CognitiveAccount
		if err := json.Unmarshal(raw, &account); err != nil {
			return err
		}
		list = append(list, account)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	if list == nil {
		list = []CognitiveAccount{}
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": list})
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("accountName")

	key := accountKey(subID, rg, name)
	if err := s.db.Delete(accountsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	// Delete all deployments associated with this account
	depPrefix := deploymentAccountPrefix(subID, rg, name)
	_ = s.db.List(deploymentsBucket, depPrefix, func(k string, raw []byte) error {
		_ = s.db.Delete(deploymentsBucket, k)
		return nil
	})

	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) listKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	name := r.PathValue("accountName")

	var account CognitiveAccount
	found, err := s.db.Get(accountsBucket, accountKey(subID, rg, name), &account)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound", fmt.Sprintf("Account %q was not found", name))
		return
	}

	h1 := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s/key1", subID, rg, name)))
	h2 := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s/key2", subID, rg, name)))

	keys := AccountKeys{
		Key1: hex.EncodeToString(h1[:]),
		Key2: hex.EncodeToString(h2[:]),
	}

	server.WriteJSON(w, http.StatusOK, keys)
}
