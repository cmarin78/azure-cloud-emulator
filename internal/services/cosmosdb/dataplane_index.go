package cosmosdb

const dataPlaneContainersBucket = "cosmosdb.dataplane.containers"

// El data-plane de documentos (documents.go) recibe requests con la URL
// "/{account}.documents/dbs/{db}/colls/{container}/docs[...]", que trae
// el nombre de la cuenta, db y container pero no la
// subscriptionId/resourceGroup que sí tienen las claves de
// containersBucket. Igual que el índice equivalente de Service Bus
// (ver internal/services/servicebus/dataplane_index.go), en vez de
// recorrer ese bucket entero en cada operación de documentos, este
// índice pequeño y plano (clave = "{account}/{db}/{container}") se
// mantiene en paralelo: se marca al crear el container ARM
// (containers.go) y se borra al eliminarlo.
func (s *Service) markDataPlaneContainer(entityPath string) error {
	return s.db.Put(dataPlaneContainersBucket, entityPath, true)
}

func (s *Service) unmarkDataPlaneContainer(entityPath string) error {
	return s.db.Delete(dataPlaneContainersBucket, entityPath)
}

func (s *Service) dataPlaneContainerExists(entityPath string) (bool, error) {
	var v bool
	return s.db.Get(dataPlaneContainersBucket, entityPath, &v)
}

func containerEntityPath(account, db, container string) string {
	return account + "/" + db + "/" + container
}
