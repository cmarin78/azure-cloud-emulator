package cognitiveservices

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const deploymentsBucket = "cognitiveservices.deployments"

// CognitiveDeployment represents a model deployment (e.g. gpt-4o, text-embedding-3-small).
type CognitiveDeployment struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Type       string               `json:"type"`
	Sku        *DeploymentSku       `json:"sku,omitempty"`
	Properties DeploymentProperties `json:"properties"`
}

// DeploymentSku represents the SKU and capacity of the deployment.
type DeploymentSku struct {
	Name     string `json:"name"`
	Capacity int    `json:"capacity,omitempty"`
}

// DeploymentModel defines the underlying AI model format, name, and version.
type DeploymentModel struct {
	Format  string `json:"format"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// DeploymentProperties details model settings and status.
type DeploymentProperties struct {
	Model                DeploymentModel `json:"model"`
	ProvisioningState    string          `json:"provisioningState"`
	RaiPolicyName        string          `json:"raiPolicyName,omitempty"`
	VersionUpgradeOption string          `json:"versionUpgradeOption,omitempty"`
}

type deploymentRequest struct {
	Sku        *DeploymentSku `json:"sku,omitempty"`
	Properties struct {
		Model                *DeploymentModel `json:"model,omitempty"`
		RaiPolicyName        string           `json:"raiPolicyName,omitempty"`
		VersionUpgradeOption string           `json:"versionUpgradeOption,omitempty"`
	} `json:"properties"`
}

func deploymentAccountPrefix(subID, rg, accountName string) string {
	return subID + "/" + rg + "/" + strings.ToLower(accountName) + "/"
}

func deploymentKey(subID, rg, accountName, deploymentName string) string {
	return deploymentAccountPrefix(subID, rg, accountName) + strings.ToLower(deploymentName)
}

func (s *Service) registerDeployments(mux *http.ServeMux) {
	const base = "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.CognitiveServices/accounts/{accountName}/deployments"

	mux.HandleFunc("GET "+base, s.listDeployments)
	mux.HandleFunc("PUT "+base+"/{deploymentName}", s.putDeployment)
	mux.HandleFunc("GET "+base+"/{deploymentName}", s.getDeployment)
	mux.HandleFunc("DELETE "+base+"/{deploymentName}", s.deleteDeployment)
}

func (s *Service) putDeployment(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	accountName := r.PathValue("accountName")
	deploymentName := r.PathValue("deploymentName")

	var account CognitiveAccount
	found, err := s.db.Get(accountsBucket, accountKey(subID, rg, accountName), &account)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound", fmt.Sprintf("Cognitive account %q was not found", accountName))
		return
	}

	var req deploymentRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			server.WriteError(w, http.StatusBadRequest, "InvalidRequestBody", "malformed request JSON body")
			return
		}
	}

	modelFormat := "OpenAI"
	modelName := "gpt-4o"
	modelVersion := "2024-05-13"
	if req.Properties.Model != nil {
		if req.Properties.Model.Format != "" {
			modelFormat = req.Properties.Model.Format
		}
		if req.Properties.Model.Name != "" {
			modelName = req.Properties.Model.Name
		}
		if req.Properties.Model.Version != "" {
			modelVersion = req.Properties.Model.Version
		}
	}

	skuName := "Standard"
	capacity := 1
	if req.Sku != nil {
		if req.Sku.Name != "" {
			skuName = req.Sku.Name
		}
		if req.Sku.Capacity > 0 {
			capacity = req.Sku.Capacity
		}
	}

	raiPolicy := "Microsoft.Default"
	if req.Properties.RaiPolicyName != "" {
		raiPolicy = req.Properties.RaiPolicyName
	}

	upgradeOption := "OnceNewDefaultVersionAvailable"
	if req.Properties.VersionUpgradeOption != "" {
		upgradeOption = req.Properties.VersionUpgradeOption
	}

	dep := CognitiveDeployment{
		ID:   fmt.Sprintf("%s/deployments/%s", account.ID, deploymentName),
		Name: deploymentName,
		Type: "Microsoft.CognitiveServices/accounts/deployments",
		Sku: &DeploymentSku{
			Name:     skuName,
			Capacity: capacity,
		},
		Properties: DeploymentProperties{
			Model: DeploymentModel{
				Format:  modelFormat,
				Name:    modelName,
				Version: modelVersion,
			},
			ProvisioningState:    "Succeeded",
			RaiPolicyName:        raiPolicy,
			VersionUpgradeOption: upgradeOption,
		},
	}

	key := deploymentKey(subID, rg, accountName, deploymentName)
	if err := s.db.Put(deploymentsBucket, key, dep); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	server.WriteJSON(w, http.StatusOK, dep)
}

func (s *Service) getDeployment(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	accountName := r.PathValue("accountName")
	deploymentName := r.PathValue("deploymentName")

	var dep CognitiveDeployment
	found, err := s.db.Get(deploymentsBucket, deploymentKey(subID, rg, accountName, deploymentName), &dep)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound", fmt.Sprintf("Deployment %q was not found", deploymentName))
		return
	}

	server.WriteJSON(w, http.StatusOK, dep)
}

func (s *Service) listDeployments(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	accountName := r.PathValue("accountName")
	prefix := deploymentAccountPrefix(subID, rg, accountName)

	var list []CognitiveDeployment
	err := s.db.List(deploymentsBucket, prefix, func(k string, raw []byte) error {
		var dep CognitiveDeployment
		if err := json.Unmarshal(raw, &dep); err != nil {
			return err
		}
		list = append(list, dep)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	if list == nil {
		list = []CognitiveDeployment{}
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": list})
}

func (s *Service) deleteDeployment(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	accountName := r.PathValue("accountName")
	deploymentName := r.PathValue("deploymentName")

	key := deploymentKey(subID, rg, accountName, deploymentName)
	if err := s.db.Delete(deploymentsBucket, key); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
