package eventhub

const dataPlaneHubsBucket = "eventhub.dataplane.hubs"

// El data-plane (dataplane.go) recibe requests con la URL
// "/{namespace}.eventhub/{eventHubName}/...", que solo trae el nombre del
// namespace y del event hub -- no la subscriptionId/resourceGroup que sí
// tiene la clave de hubsBucket. Este índice plano (clave =
// "{namespace}/{eventHubName}") permite confirmar que el event hub existe
// sin recorrer todo el bucket ARM -- mismo patrón que
// servicebus/dataplane_index.go.
func (s *Service) markDataPlaneHub(entityPath string) error {
	return s.db.Put(dataPlaneHubsBucket, entityPath, true)
}

func (s *Service) unmarkDataPlaneHub(entityPath string) error {
	return s.db.Delete(dataPlaneHubsBucket, entityPath)
}

func (s *Service) dataPlaneHubExists(entityPath string) (bool, error) {
	var v bool
	return s.db.Get(dataPlaneHubsBucket, entityPath, &v)
}

func hubEntityPath(namespace, hub string) string {
	return namespace + "/" + hub
}
