package compute

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/cesarmarin/azure-emulator/internal/server"
)

// imageCatalog es el catálogo estático de imágenes de VM que este emulador
// soporta, indexado por publisher/offer/sku -> lista de versiones
// (ordenadas de más vieja a más nueva; la última es "latest"). No hay
// CRUD aquí: az/Terraform solo leen este catálogo (vía az vm image list-skus
// o al resolver properties.storageProfile.imageReference de una VM), igual
// que el roadmap pide un "catálogo estático", no un recurso administrable.
var imageCatalog = map[string]map[string]map[string][]string{
	"Canonical": {
		"0001-com-ubuntu-server-jammy": {
			"22_04-lts-gen2": {"22.04.202401010"},
		},
	},
	"MicrosoftWindowsServer": {
		"WindowsServer": {
			"2022-datacenter-azure-edition": {"20348.2402.231109"},
		},
	},
}

// ImageReference replica "properties.storageProfile.imageReference" de una
// VM (también es la forma que devuelve el endpoint de versiones debajo).
type ImageReference struct {
	Publisher string `json:"publisher"`
	Offer     string `json:"offer"`
	Sku       string `json:"sku"`
	Version   string `json:"version"`
}

// imageVersion replica el recurso que devuelve el endpoint real de ARM
// .../offers/{offer}/skus/{sku}/versions[/{version}] (usado por
// `az vm image list`/`list-skus`/`show`).
type imageVersion struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
}

func (s *Service) registerImages(mux *http.ServeMux) {
	mux.HandleFunc(
		"GET /subscriptions/{subscriptionId}/providers/Microsoft.Compute/locations/{location}/publishers/{publisher}/artifacttypes/vmimage/offers/{offer}/skus/{sku}/versions",
		s.listImageVersions)
}

func (s *Service) listImageVersions(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.RequireAPIVersion(w, r); !ok {
		return
	}
	subID := r.PathValue("subscriptionId")
	location := r.PathValue("location")
	publisher := r.PathValue("publisher")
	offer := r.PathValue("offer")
	sku := r.PathValue("sku")

	versions := catalogVersions(publisher, offer, sku)
	if versions == nil {
		server.WriteError(w, http.StatusNotFound, "ResourceNotFound",
			fmt.Sprintf("no hay imágenes en el catálogo para publisher=%s offer=%s sku=%s", publisher, offer, sku))
		return
	}

	result := make([]imageVersion, 0, len(versions))
	for _, v := range versions {
		result = append(result, imageVersion{
			ID: fmt.Sprintf(
				"/subscriptions/%s/providers/Microsoft.Compute/locations/%s/publishers/%s/artifacttypes/vmimage/offers/%s/skus/%s/versions/%s",
				subID, location, publisher, offer, sku, v),
			Name:     v,
			Location: location,
		})
	}
	server.WriteJSON(w, http.StatusOK, map[string]any{"value": result})
}

// catalogVersions devuelve las versiones publicadas para
// publisher/offer/sku, o nil si la combinación no está en el catálogo.
func catalogVersions(publisher, offer, sku string) []string {
	offers, ok := imageCatalog[publisher]
	if !ok {
		return nil
	}
	skus, ok := offers[offer]
	if !ok {
		return nil
	}
	versions, ok := skus[sku]
	if !ok {
		return nil
	}
	return versions
}

// resolveImageReference valida una imageReference contra el catálogo
// estático y resuelve "latest" a la versión más reciente conocida. Lo usa
// vms.go al crear una VM.
func resolveImageReference(ref ImageReference) (ImageReference, error) {
	if strings.TrimSpace(ref.Publisher) == "" || strings.TrimSpace(ref.Offer) == "" || strings.TrimSpace(ref.Sku) == "" {
		return ImageReference{}, fmt.Errorf("publisher, offer y sku son obligatorios en storageProfile.imageReference")
	}
	versions := catalogVersions(ref.Publisher, ref.Offer, ref.Sku)
	if versions == nil {
		return ImageReference{}, fmt.Errorf(
			"no hay ninguna imagen en el catálogo para publisher=%q offer=%q sku=%q (catálogo estático, ver internal/services/compute/images.go)",
			ref.Publisher, ref.Offer, ref.Sku)
	}

	version := ref.Version
	if version == "" || version == "latest" {
		version = versions[len(versions)-1]
	} else if !containsVersion(versions, version) {
		return ImageReference{}, fmt.Errorf(
			"la versión %q no existe para publisher=%q offer=%q sku=%q; versiones disponibles: %s",
			version, ref.Publisher, ref.Offer, ref.Sku, strings.Join(versions, ", "))
	}

	resolved := ref
	resolved.Version = version
	return resolved, nil
}

func containsVersion(versions []string, v string) bool {
	for _, candidate := range versions {
		if candidate == v {
			return true
		}
	}
	return false
}
