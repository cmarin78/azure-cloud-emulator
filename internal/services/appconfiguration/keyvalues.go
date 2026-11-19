package appconfiguration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

const keyValuesBucket = "appconfiguration.keyvalues"

// KeyValue represents an individual configuration key-value pair.
type KeyValue struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Key          string            `json:"key"`
	Label        string            `json:"label,omitempty"`
	Value        string            `json:"value"`
	ContentType  string            `json:"contentType,omitempty"`
	ETag         string            `json:"eTag,omitempty"`
	LastModified string            `json:"lastModified,omitempty"`
	Locked       bool              `json:"locked"`
	Tags         map[string]string `json:"tags,omitempty"`
}

type keyValueRequest struct {
	Value       string            `json:"value"`
	ContentType string            `json:"contentType,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

func kvStorePrefix(subID, rg, storeName string) string {
	return subID + "/" + rg + "/" + strings.ToLower(storeName) + "/"
}

func kvKey(subID, rg, storeName, kvName string) string {
	return kvStorePrefix(subID, rg, storeName) + strings.ToLower(kvName)
}

func (s *Service) registerKeyValues(mux *http.ServeMux) {
	const base = "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.AppConfiguration/configurationStores/{configStoreName}/keyValues"

	mux.HandleFunc("GET "+base, s.listKeyValues)
	mux.HandleFunc("PUT "+base+"/{keyValueName}", s.putKeyValue)
	mux.HandleFunc("GET "+base+"/{keyValueName}", s.getKeyValue)
	mux.HandleFunc("DELETE "+base+"/{keyValueName}", s.deleteKeyValue)
}

func (s *Service) putKeyValue(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	storeName := r.PathValue("configStoreName")
	kvName := r.PathValue("keyValueName")

	var store ConfigurationStore
	found, err := s.db.Get(storesBucket, storeKey(subID, rg, storeName), &store)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound", fmt.Sprintf("Configuration store %q was not found", storeName))
		return
	}

	var req keyValueRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			server.WriteError(w, http.StatusBadRequest, "InvalidRequestBody", "malformed request JSON body")
			return
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	hash := sha256.Sum256([]byte(req.Value + now))
	etag := hex.EncodeToString(hash[:8])

	// keyValueName can be key$label or just key
	keyPart := kvName
	labelPart := ""
	if idx := strings.Index(kvName, "$"); idx >= 0 {
		keyPart = kvName[:idx]
		labelPart = kvName[idx+1:]
	}

	kv := KeyValue{
		ID:           fmt.Sprintf("%s/keyValues/%s", store.ID, kvName),
		Name:         kvName,
		Type:         "Microsoft.AppConfiguration/configurationStores/keyValues",
		Key:          keyPart,
		Label:        labelPart,
		Value:        req.Value,
		ContentType:  req.ContentType,
		ETag:         etag,
		LastModified: now,
		Locked:       false,
		Tags:         req.Tags,
	}

	if err := s.db.Put(keyValuesBucket, kvKey(subID, rg, storeName, kvName), kv); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	server.WriteJSON(w, http.StatusOK, kv)
}

func (s *Service) getKeyValue(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	storeName := r.PathValue("configStoreName")
	kvName := r.PathValue("keyValueName")

	var kv KeyValue
	found, err := s.db.Get(keyValuesBucket, kvKey(subID, rg, storeName, kvName), &kv)
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if !found {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound", fmt.Sprintf("Key-value %q was not found", kvName))
		return
	}

	server.WriteJSON(w, http.StatusOK, kv)
}

func (s *Service) listKeyValues(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	storeName := r.PathValue("configStoreName")
	prefix := kvStorePrefix(subID, rg, storeName)

	var list []KeyValue
	err := s.db.List(keyValuesBucket, prefix, func(k string, raw []byte) error {
		var kv KeyValue
		if err := json.Unmarshal(raw, &kv); err != nil {
			return err
		}
		list = append(list, kv)
		return nil
	})
	if err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	if list == nil {
		list = []KeyValue{}
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": list})
}

func (s *Service) deleteKeyValue(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}

	subID := r.PathValue("subscriptionId")
	rg := r.PathValue("resourceGroupName")
	storeName := r.PathValue("configStoreName")
	kvName := r.PathValue("keyValueName")

	k := kvKey(subID, rg, storeName, kvName)
	if err := s.db.Delete(keyValuesBucket, k); err != nil {
		server.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ServeDataPlane handles data-plane requests directed at /{storeName}.appconfig/kv/{key...}
func (s *Service) ServeDataPlane(w http.ResponseWriter, r *http.Request, storeName, subPath string) {
	var fullKey string
	found, err := s.db.Get(dataPlaneBucket, strings.ToLower(storeName), &fullKey)
	if err != nil || !found {
		http.Error(w, "App Configuration store not found", http.StatusNotFound)
		return
	}

	parts := strings.Split(fullKey, "/")
	if len(parts) < 3 {
		http.Error(w, "corrupt dataplane index", http.StatusInternalServerError)
		return
	}
	subID, rg, sName := parts[0], parts[1], parts[2]

	cleanPath := strings.TrimPrefix(subPath, "/")
	if !strings.HasPrefix(cleanPath, "kv") {
		http.Error(w, "unsupported data plane path", http.StatusNotFound)
		return
	}

	keyParam := strings.TrimPrefix(cleanPath, "kv")
	keyParam = strings.TrimPrefix(keyParam, "/")

	label := r.URL.Query().Get("label")
	targetName := keyParam
	if label != "" {
		targetName = keyParam + "$" + label
	}

	switch r.Method {
	case http.MethodGet:
		if keyParam == "" {
			// List keys
			var list []KeyValue
			prefix := kvStorePrefix(subID, rg, sName)
			_ = s.db.List(keyValuesBucket, prefix, func(k string, raw []byte) error {
				var kv KeyValue
				if err := json.Unmarshal(raw, &kv); err == nil {
					list = append(list, kv)
				}
				return nil
			})
			if list == nil {
				list = []KeyValue{}
			}
			server.WriteJSON(w, http.StatusOK, map[string]any{"items": list})
			return
		}

		var kv KeyValue
		found, err := s.db.Get(keyValuesBucket, kvKey(subID, rg, sName, targetName), &kv)
		if err != nil || !found {
			http.Error(w, "Key not found", http.StatusNotFound)
			return
		}
		server.WriteJSON(w, http.StatusOK, kv)

	case http.MethodPut:
		var body struct {
			Value       string            `json:"value"`
			ContentType string            `json:"content_type"`
			Tags        map[string]string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		now := time.Now().UTC().Format(time.RFC3339)
		hash := sha256.Sum256([]byte(body.Value + now))
		etag := hex.EncodeToString(hash[:8])

		kv := KeyValue{
			ID:           fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.AppConfiguration/configurationStores/%s/keyValues/%s", subID, rg, sName, targetName),
			Name:         targetName,
			Key:          keyParam,
			Label:        label,
			Value:        body.Value,
			ContentType:  body.ContentType,
			ETag:         etag,
			LastModified: now,
			Tags:         body.Tags,
		}

		if err := s.db.Put(keyValuesBucket, kvKey(subID, rg, sName, targetName), kv); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		server.WriteJSON(w, http.StatusOK, kv)

	case http.MethodDelete:
		_ = s.db.Delete(keyValuesBucket, kvKey(subID, rg, sName, targetName))
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}
