package servicebus

const dataPlaneEntitiesBucket = "servicebus.dataplane.entities"

// El data-plane (messaging.go) recibe requests con la URL
// "/{namespace}.servicebus/{resto}", que solo trae el nombre del
// namespace — no la subscriptionId/resourceGroup que sí tienen las claves
// de queuesBucket/topicsBucket/subscriptionsBucket. En vez de recorrer
// esos buckets enteros en cada mensaje enviado/recibido, este índice
// pequeño y plano (clave = "{namespace}/{queue}" o
// "{namespace}/{topic}" o "{namespace}/{topic}/subscriptions/{sub}")
// se mantiene en paralelo: se marca al crear el sub-recurso ARM
// (queues.go/topics.go) y se borra al eliminarlo.
func (s *Service) markDataPlaneEntity(entityPath string) error {
	return s.db.Put(dataPlaneEntitiesBucket, entityPath, true)
}

func (s *Service) unmarkDataPlaneEntity(entityPath string) error {
	return s.db.Delete(dataPlaneEntitiesBucket, entityPath)
}

func (s *Service) dataPlaneEntityExists(entityPath string) (bool, error) {
	var v bool
	return s.db.Get(dataPlaneEntitiesBucket, entityPath, &v)
}

func queueEntityPath(namespace, queue string) string {
	return namespace + "/" + queue
}

func topicEntityPath(namespace, topic string) string {
	return namespace + "/" + topic
}

func subscriptionEntityPath(namespace, topic, sub string) string {
	return namespace + "/" + topic + "/subscriptions/" + sub
}
