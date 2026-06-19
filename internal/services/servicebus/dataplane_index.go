package servicebus

const dataPlaneEntitiesBucket = "servicebus.dataplane.entities"

// El data-plane (messaging.go) recibe requests con la URL
// "/{namespace}.servicebus/{resto}", que solo trae el nombre del
// namespace — no la subscriptionId/resourceGroup que sí tienen las claves
// de queuesBucket/topicsBucket/subscriptionsBucket. En vez de recorrer
// esos buckets enteros en cada mensaje enviado/recibido, este índice
// pequeño y plano (clave = "{namespace}/queues/{queue}" o
// "{namespace}/topics/{topic}" o "{namespace}/{topic}/subscriptions/{sub}")
// se mantiene en paralelo: se marca al crear el sub-recurso ARM
// (queues.go/topics.go) y se borra al eliminarlo.
//
// Nota: queueEntityPath y topicEntityPath deben usar prefijos distintos
// ("queues/" vs "topics/") porque comparten el mismo bucket plano — si
// ambos devolvieran "{namespace}/{nombre}" sin distinguir, una cola y un
// topic con namespace+nombre coincidentes (o, peor, cualquier topic visto
// desde isQueue) colisionarían en la misma clave.
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
	return namespace + "/queues/" + queue
}

func topicEntityPath(namespace, topic string) string {
	return namespace + "/topics/" + topic
}

func subscriptionEntityPath(namespace, topic, sub string) string {
	return namespace + "/" + topic + "/subscriptions/" + sub
}
